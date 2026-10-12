package main

import (
	"strings"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/message"
)

func withEnv(t *testing.T, m map[string]string) {
	old := config.Env
	t.Cleanup(func() { config.Env = old })
	config.Env = func(k string) string { return m[k] }
}

func TestLoadSettings(t *testing.T) {
	withEnv(t, map[string]string{})
	if _, err := loadSettings(); err == nil {
		t.Error("want error without POSTGRES_URL")
	}
	withEnv(t, map[string]string{"POSTGRES_URL": "postgres://x", "POSTGRES_TABLE": "iot.shadows"})
	s, err := loadSettings()
	if err != nil || s.Table.Sanitize() != `"iot"."shadows"` || s.BatchSize != 500 || s.Timescale {
		t.Errorf("settings = %+v, err = %v", s, err)
	}
	withEnv(t, map[string]string{"POSTGRES_URL": "postgres://x", "POSTGRES_TABLE": "a.b.c"})
	if _, err := loadSettings(); err == nil {
		t.Error("want error for a.b.c")
	}
}

func TestSchemaSQL(t *testing.T) {
	tbl, _ := parseTable("iot.shadows")
	q := strings.Join(schemaSQL(&settings{Table: tbl, Timescale: true, ChunkDays: 3}), ";")
	for _, want := range []string{"CREATE EXTENSION IF NOT EXISTS timescaledb", `CREATE SCHEMA IF NOT EXISTS "iot"`,
		`CREATE TABLE IF NOT EXISTS "iot"."shadows"`, `"shadows_device_time_idx"`, `create_hypertable('"iot"."shadows"'`, "INTERVAL '3 days'"} {
		if !strings.Contains(q, want) {
			t.Errorf("missing %q in %s", want, q)
		}
	}
	tbl, _ = parseTable("plain")
	if q := strings.Join(schemaSQL(&settings{Table: tbl}), ";"); strings.Contains(q, "timescale") || strings.Contains(q, "SCHEMA") {
		t.Errorf("plain table should not touch timescale or schemas: %s", q)
	}
}

func TestRow(t *testing.T) {
	m := &message.Message{Device: "d1", Reported: &message.State{Timestamp: "2026-10-12T08:00:00Z", Data: map[string]any{"temp": 20.0}},
		Connection: &message.Connection{Connected: true}, SentAt: "2026-10-12T09:00:00Z"}
	r := row(m)
	if r[0].(time.Time).Hour() != 8 || r[1] != "d1" || r[2] != nil || r[3] != `{"temp":20}` || r[4] != nil || r[5] != true {
		t.Errorf("row = %v", r)
	}
}
