package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/stream/consumer"
	sf "github.com/snowflakedb/gosnowflake"
)

const (
	envSnowflakeAccount   = "SNOWFLAKE_ACCOUNT"
	envSnowflakeUser      = "SNOWFLAKE_USER"
	envSnowflakePassword  = "SNOWFLAKE_PASSWORD"
	envSnowflakeInitDB    = "SNOWFLAKE_INITDB"
	envSnowflakeWarehouse = "SNOWFLAKE_WAREHOUSE"
	envSnowflakeRole      = "SNOWFLAKE_ROLE"
	envSnowflakeSchema    = "SNOWFLAKE_SCHEMA"
)

const insertStmt = "INSERT INTO device_states (uid, timestamp, version, data) SELECT ?, ?, ?, PARSE_JSON(?)"

func main() {
	account := os.Getenv(envSnowflakeAccount)
	user := os.Getenv(envSnowflakeUser)
	password := os.Getenv(envSnowflakePassword)
	dsn, err := sf.DSN(&sf.Config{
		Account:   account,
		User:      user,
		Password:  password,
		Warehouse: os.Getenv(envSnowflakeWarehouse),
		Role:      os.Getenv(envSnowflakeRole),
	})
	if err != nil {
		log.Fatalf("failed to create DSN, err: %v", err)
	}

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		log.Fatalf("failed to connect to Snowflake account %s, err: %v", account, err)
	}
	defer db.Close()

	shouldInitDB, err := config.Bool(envSnowflakeInitDB, false)
	if err != nil {
		log.Fatal(err)
	}
	if err := prepareDB(db, shouldInitDB, os.Getenv(envSnowflakeSchema)); err != nil {
		log.Fatal(err)
	}

	redisPool, err := config.RedisPool()
	if err != nil {
		log.Fatal(err)
	}
	consumer.New(redisPool).Run(func(event *consumer.DeviceEvent) error {
		if err := insertDeviceEvent(db, event); err != nil {
			log.Printf("failed to insert event to Snowflake: %v", err)
			return err
		}
		log.Println("inserted event to Snowflake:", event)
		return nil
	})
}

// quoteIdent quotes a Snowflake identifier.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// prepareDB selects the infinimesh database and the schema. With init it
// first creates the database, the schema and the table if they do not exist.
func prepareDB(db *sql.DB, init bool, schema string) error {
	var stmts []string
	if init {
		stmts = append(stmts, `CREATE DATABASE IF NOT EXISTS infinimesh;`)
	}
	stmts = append(stmts, `USE DATABASE infinimesh;`)
	if schema != "" {
		// identifiers cannot be bind parameters, so quote it as one
		if init {
			stmts = append(stmts, `CREATE SCHEMA IF NOT EXISTS `+quoteIdent(schema)+`;`)
		}
		stmts = append(stmts, `USE SCHEMA `+quoteIdent(schema)+`;`)
	}
	if init {
		stmts = append(stmts, `CREATE TABLE IF NOT EXISTS device_states (
			uid VARCHAR(1000),
			timestamp TIMESTAMP,
			version VARCHAR(1000),
			data VARIANT,
			PRIMARY KEY (uid, timestamp)
		);`)
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to run %q: %w", stmt, err)
		}
	}
	return nil
}

func insertDeviceEvent(db *sql.DB, event *consumer.DeviceEvent) error {
	t, err := time.Parse(time.RFC3339Nano, event.State.Timestamp)
	if err != nil {
		// a bad timestamp will not get better on retry, so drop the event
		log.Printf("skipping event with bad timestamp %q: %v", event.State.Timestamp, err)
		return nil
	}
	data, err := json.Marshal(event.State.Data)
	if err != nil {
		return err
	}
	// all values are bind parameters, so device data cannot change the SQL
	_, err = db.Exec(insertStmt, event.Object.UID, t, event.State.Version, string(data))
	return err
}
