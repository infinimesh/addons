package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(envMap(map[string]string{"API_URL": "http://api:8000", "KAFKA_BROKERS": "a:9092, b:9092"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Brokers) != 2 || c.Brokers[1] != "b:9092" {
		t.Errorf("brokers = %v", c.Brokers)
	}
	if c.MaxVersion != nil {
		t.Error("no version cap by default")
	}
	if c.Topic != "infinimesh.shadows" || c.Idempotent || !c.CreateTopic || c.Partitions != 1 {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	for _, m := range []map[string]string{
		{"KAFKA_BROKERS": "a:9092"},
		{"API_URL": "x"},
		{"API_URL": "x", "KAFKA_BROKERS": "a:9092", "KAFKA_SASL_MECHANISM": "GSSAPI"},
		{"API_URL": "x", "KAFKA_BROKERS": "a:9092", "KAFKA_TLS": "maybe"},
		{"API_URL": "x", "KAFKA_BROKERS": "a:9092", "KAFKA_MAX_VERSION": "latest"},
	} {
		if _, err := loadConfig(envMap(m)); err == nil {
			t.Errorf("want error for %v", m)
		}
	}
}

func TestBuildMessage(t *testing.T) {
	data, _ := structpb.NewStruct(map[string]interface{}{"temp": 21.5})
	obj := api.Object{UID: "d1", Name: "pump"}
	s := &shadowpb.Shadow{Device: "d1", Reported: &shadowpb.State{Data: data}, Connection: &shadowpb.ConnectionState{Connected: true}}
	key, value, ok := buildMessage(obj, s, false, time.Unix(0, 0))
	if !ok || string(key) != "d1" {
		t.Fatalf("key = %q ok = %v", key, ok)
	}
	var m Message
	if err := json.Unmarshal(value, &m); err != nil {
		t.Fatal(err)
	}
	if m.Reported.Data["temp"] != 21.5 || m.Connection == nil || !m.Connection.Connected || m.Title != "pump" {
		t.Errorf("message = %s", value)
	}
	_, value, ok = buildMessage(obj, s, true, time.Unix(0, 0))
	if !ok {
		t.Fatal("want message")
	}
	m = Message{}
	_ = json.Unmarshal(value, &m)
	if m.Connection != nil {
		t.Error("KAFKA_ONLY_REPORTED should drop connection state")
	}
	if _, _, ok := buildMessage(obj, &shadowpb.Shadow{Device: "d1"}, false, time.Now()); ok {
		t.Error("empty shadow should not be sent")
	}
}

func TestMaxVersion(t *testing.T) {
	c, err := loadConfig(envMap(map[string]string{"API_URL": "x", "KAFKA_BROKERS": "a:9092", "KAFKA_MAX_VERSION": "3.9"}))
	if err != nil || c.MaxVersion == nil {
		t.Fatalf("cap = %v, err = %v", c, err)
	}
	if got := len(c.kafkaOpts()); got != 6 {
		t.Errorf("opts = %d, want 6 with the cap", got)
	}
}
