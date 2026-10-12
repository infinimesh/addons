package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type settings struct {
	URL          string
	Table        pgx.Identifier
	Timescale    bool
	ChunkDays    int
	BatchSize    int
	Flush        time.Duration
	MaxBuffer    int
	OnlyReported bool
}

func loadSettings() (*settings, error) {
	s := &settings{URL: config.Env("POSTGRES_URL")}
	if s.URL == "" {
		return nil, fmt.Errorf("POSTGRES_URL is required")
	}
	var err error
	if s.Table, err = parseTable(config.String("POSTGRES_TABLE", "device_shadows")); err != nil {
		return nil, err
	}
	if s.Timescale, err = config.Bool("POSTGRES_TIMESCALE", false); err != nil {
		return nil, err
	}
	if s.ChunkDays, err = config.Int("POSTGRES_TIMESCALE_CHUNK_DAYS", 7); err != nil {
		return nil, err
	}
	if s.BatchSize, err = config.Int("POSTGRES_BATCH_SIZE", 500); err != nil {
		return nil, err
	}
	if s.Flush, err = config.Seconds(time.Second, "POSTGRES_FLUSH_INTERVAL_SECS"); err != nil {
		return nil, err
	}
	if s.MaxBuffer, err = config.Int("POSTGRES_MAX_BUFFER", 100000); err != nil {
		return nil, err
	}
	if s.OnlyReported, err = config.Bool("POSTGRES_ONLY_REPORTED", false); err != nil {
		return nil, err
	}
	if s.BatchSize < 1 || s.ChunkDays < 1 || s.MaxBuffer < s.BatchSize {
		return nil, fmt.Errorf("POSTGRES_BATCH_SIZE and POSTGRES_TIMESCALE_CHUNK_DAYS must be at least 1, POSTGRES_MAX_BUFFER at least the batch size")
	}
	return s, nil
}

// parseTable reads "table" or "schema.table".
func parseTable(v string) (pgx.Identifier, error) {
	parts := strings.Split(v, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("POSTGRES_TABLE must be table or schema.table")
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("POSTGRES_TABLE must be table or schema.table")
		}
	}
	return pgx.Identifier(parts), nil
}

var columns = []string{"time", "device", "title", "reported", "desired", "connected"}

// schemaSQL is run on start. Every statement is safe to run again.
func schemaSQL(s *settings) []string {
	t := s.Table.Sanitize()
	idx := pgx.Identifier{s.Table[len(s.Table)-1] + "_device_time_idx"}.Sanitize()
	stmts := []string{}
	if s.Timescale {
		stmts = append(stmts, "CREATE EXTENSION IF NOT EXISTS timescaledb")
	}
	if len(s.Table) == 2 {
		stmts = append(stmts, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{s.Table[0]}.Sanitize())
	}
	stmts = append(stmts,
		`CREATE TABLE IF NOT EXISTS `+t+` (
	time timestamptz NOT NULL,
	device text NOT NULL,
	title text,
	reported jsonb,
	desired jsonb,
	connected boolean
)`,
		"CREATE INDEX IF NOT EXISTS "+idx+" ON "+t+" (device, time DESC)",
	)
	if s.Timescale {
		stmts = append(stmts, fmt.Sprintf("SELECT create_hypertable('%s', 'time', chunk_time_interval => INTERVAL '%d days', if_not_exists => TRUE, migrate_data => TRUE)",
			strings.ReplaceAll(t, "'", "''"), s.ChunkDays))
	}
	return stmts
}

func setup(ctx context.Context, pool *pgxpool.Pool, s *settings) error {
	for _, q := range schemaSQL(s) {
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", strings.SplitN(q, "\n", 2)[0], err)
		}
	}
	return nil
}

func jsonOrNil(st *message.State) []byte {
	if st == nil {
		return nil
	}
	b, err := json.Marshal(st.Data)
	if err != nil {
		return nil
	}
	return b
}

// row turns a message into the values for one row, in the order of columns.
func row(m *message.Message) []any {
	var connected any
	if m.Connection != nil {
		connected = m.Connection.Connected
	}
	var title any
	if m.Title != "" {
		title = m.Title
	}
	var rep, des any
	if b := jsonOrNil(m.Reported); b != nil {
		rep = string(b)
	}
	if b := jsonOrNil(m.Desired); b != nil {
		des = string(b)
	}
	return []any{m.Time(), m.Device, title, rep, des, connected}
}

// insert writes all rows with COPY in one round trip.
func insert(ctx context.Context, pool *pgxpool.Pool, table pgx.Identifier, rows [][]any) error {
	_, err := pool.CopyFrom(ctx, table, columns, pgx.CopyFromRows(rows))
	return err
}
