package main

import (
	"testing"

	"github.com/infinimesh/addons/sdk/api"
)

func TestCSVRecordMapping(t *testing.T) {
	obj := api.Object{UID: "d1", Name: "pump", Kind: "device"}
	state := &api.DevicesStateRes{Result: api.DevicesStateResult{ReportedState: api.DeviceState{
		Timestamp: "2026-10-12T08:00:00Z",
		Data:      map[string]interface{}{"temp": 21.5},
	}}}
	got := csvRecord(obj, state)
	want := []string{"d1", "pump", "device", "2026-10-12T08:00:00Z", "", `{"temp":21.5}`}
	if len(got) != len(want) {
		t.Fatalf("record = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, got[i], want[i])
		}
	}
}
