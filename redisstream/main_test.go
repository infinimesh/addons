package main

import (
	"encoding/json"
	"testing"

	"github.com/infinimesh/addons/sdk/api"
)

func TestEntryMapping(t *testing.T) {
	obj := api.Object{UID: "d1", Name: "pump", Kind: "device", Tags: []string{"a"}, Namespace: "ns1", NamespaceName: "plant"}
	state := &api.DevicesStateRes{Result: api.DevicesStateResult{ReportedState: api.DeviceState{
		Timestamp: "2026-10-12T08:00:00Z",
		Data:      map[string]interface{}{"temp": 21.5},
	}}}
	args := entryArgs(1000, obj, state)
	if len(args) != 9 || args[0] != "objects" || args[1] != "MAXLEN" || args[3] != 1000 || args[4] != "*" || args[5] != "object" || args[7] != "state" {
		t.Fatalf("args = %v", args)
	}
	var gotObj api.Object
	if err := json.Unmarshal([]byte(args[6].(string)), &gotObj); err != nil {
		t.Fatal(err)
	}
	if gotObj.UID != "d1" || gotObj.Name != "pump" || gotObj.Kind != "device" || gotObj.Namespace != "ns1" || gotObj.NamespaceName != "plant" || len(gotObj.Tags) != 1 {
		t.Errorf("object = %+v", gotObj)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(args[8].(string)), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["timestamp"] != "2026-10-12T08:00:00Z" || raw["version"] != "" || raw["data"].(map[string]interface{})["temp"] != 21.5 {
		t.Errorf("state = %v", raw)
	}
	if a := entryArgs(0, obj, state); len(a) != 6 || a[1] != "*" {
		t.Errorf("no maxlen args = %v", a)
	}
}
