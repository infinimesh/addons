package main

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/SAP/go-hdb/driver"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/stream/consumer"
)

const (
	envSapHanaInitDB   = "SAPHANA_INITDB"
	envSapHanaUsername = "SAPHANA_USERNAME"
	envSapHanaPassword = "SAPHANA_PASSWORD"
	envSapHanaHost     = "SAPHANA_HOST"
	envSapHanaPort     = "SAPHANA_PORT"
	envSapHanaTLS      = "SAPHANA_TLS"
)

const (
	tableExistsStmt = `SELECT COUNT(*) FROM SYS.TABLES WHERE SCHEMA_NAME = CURRENT_SCHEMA AND TABLE_NAME = 'DEVICES'`
	createTableStmt = `CREATE TABLE devices (uid NVARCHAR(1000), timestamp TIMESTAMP, version NVARCHAR(1000), data NCLOB, PRIMARY KEY (uid, timestamp))`
	insertStmt      = `INSERT INTO devices (uid, timestamp, version, data) VALUES (?, ?, ?, ?)`
)

func main() {
	useTLS, err := config.Bool(envSapHanaTLS, true)
	if err != nil {
		log.Fatal(err)
	}
	db := newSapHanaDB(
		os.Getenv(envSapHanaHost),
		os.Getenv(envSapHanaPort),
		os.Getenv(envSapHanaUsername),
		os.Getenv(envSapHanaPassword),
		useTLS,
	)
	shouldInitDB, err := config.Bool(envSapHanaInitDB, false)
	if err != nil {
		log.Fatal(err)
	}
	if shouldInitDB {
		if err := initDB(db); err != nil {
			log.Fatalf("failed to initialise db: err=%v\n", err)
		}
	}
	redisPool, err := config.RedisPool()
	if err != nil {
		log.Fatal(err)
	}
	consumer.New(redisPool).Run(func(event *consumer.DeviceEvent) error {
		if err := insertDeviceEvent(db, event); err != nil {
			log.Printf("failed to insert event to SAP HANA: err=%v", err)
			return err
		}
		log.Println("inserted event to SAP HANA:", event)
		return nil
	})
}

// for some reason, connecting to the db via connection string as mentioned in
// the go-hdb driver documentation doesn't work, thus we opt for this
// alternative connection method
//
// also see https://stackoverflow.com/questions/58698188
func newSapHanaDB(host, port, username, password string, useTLS bool) *sql.DB {
	c := driver.NewBasicAuthConnector(host+":"+port, username, password)
	if useTLS {
		c.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
	}
	return sql.OpenDB(c)
}

// initDB creates the devices table when it does not exist yet.
//
// ideally we would want to use the SAP HANA JSON Docstore, but the cloud
// offering didn't support it when this was written. Here we store the data
// as a JSON string in an NCLOB column.
//
// also see https://answers.sap.com/questions/13207475/
func initDB(db *sql.DB) error {
	var n int
	if err := db.QueryRow(tableExistsStmt).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		log.Println("table devices already exists")
		return nil
	}
	_, err := db.Exec(createTableStmt)
	return err
}

func insertDeviceEvent(db *sql.DB, event *consumer.DeviceEvent) error {
	args, ok, err := rowArgs(event)
	if !ok || err != nil {
		return err
	}
	_, err = db.Exec(insertStmt, args...)
	return err
}

// rowArgs maps an event to the insertStmt arguments: uid, timestamp, version
// and data as JSON. ok is false for an event to drop.
func rowArgs(event *consumer.DeviceEvent) (args []interface{}, ok bool, err error) {
	t, err := time.Parse(time.RFC3339Nano, event.State.Timestamp)
	if err != nil {
		// a bad timestamp will not get better on retry, so drop the event
		log.Printf("skipping event with bad timestamp %q: %v", event.State.Timestamp, err)
		return nil, false, nil
	}
	jsonData, err := json.Marshal(event.State.Data)
	if err != nil {
		return nil, false, err
	}
	return []interface{}{event.Object.UID, t, event.State.Version, string(jsonData)}, true, nil
}
