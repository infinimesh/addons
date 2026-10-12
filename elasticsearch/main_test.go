package main

import (
	"encoding/json"
	"testing"

	"github.com/infinimesh/addons/sdk/api"
)

func TestBuildDocument(t *testing.T) {
	obj := api.Object{UID: "d1", Name: "pump", Kind: "device"}
	id1, body, err := buildDocument(obj, api.DeviceState{Timestamp: "2026-10-12T08:00:00.1Z", Data: map[string]interface{}{"temp": 20.0}})
	if err != nil {
		t.Fatal(err)
	}
	id2, _, _ := buildDocument(obj, api.DeviceState{Timestamp: "2026-10-12T08:00:00.2Z"})
	if id1 == id2 {
		t.Errorf("ids in the same second should differ: %s", id1)
	}
	var d document
	_ = json.Unmarshal(body, &d)
	if d.UID != "d1" || d.Name != "pump" || d.Data["temp"] != 20.0 {
		t.Errorf("body = %s", body)
	}
	if _, _, err := buildDocument(obj, api.DeviceState{Timestamp: "bad"}); err == nil {
		t.Error("want error for bad timestamp")
	}
}
