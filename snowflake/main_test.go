package main

import "testing"

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("quoteIdent = %s", got)
	}
}

func TestInsertUsesOnlyBindParameters(t *testing.T) {
	if insertStmt != "INSERT INTO device_states (uid, timestamp, version, data) SELECT ?, ?, ?, PARSE_JSON(?)" {
		t.Errorf("insertStmt = %s", insertStmt)
	}
}
