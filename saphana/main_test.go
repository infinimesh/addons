package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/stream/consumer"
)

func TestRowMapping(t *testing.T) {
	ev := &consumer.DeviceEvent{
		Object: api.Object{UID: "d1"},
		State:  api.DeviceState{Timestamp: "2026-10-12T08:00:00.123Z", Data: map[string]interface{}{"temp": 21.5}},
	}
	args, ok, err := rowArgs(ev)
	if err != nil || !ok || len(args) != 4 {
		t.Fatalf("rowArgs = %v, %v, %v", args, ok, err)
	}
	if args[0] != "d1" {
		t.Errorf("uid = %v", args[0])
	}
	if ts := args[1].(time.Time); !ts.Equal(time.Date(2026, 10, 12, 8, 0, 0, 123e6, time.UTC)) {
		t.Errorf("timestamp = %v", ts)
	}
	if args[2] != "" {
		t.Errorf("version = %v", args[2])
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(args[3].(string)), &data); err != nil || data["temp"] != 21.5 {
		t.Errorf("data = %v, %v", args[3], err)
	}

	ev.State.Timestamp = "not a time"
	if _, ok, err := rowArgs(ev); ok || err != nil {
		t.Errorf("bad timestamp: ok = %v, err = %v; want the event dropped", ok, err)
	}
}
