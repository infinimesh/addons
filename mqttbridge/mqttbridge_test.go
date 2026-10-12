package main

import (
	"testing"

	"github.com/infinimesh/addons/sdk/config"
)

func withEnv(t *testing.T, m map[string]string) {
	old := config.Env
	t.Cleanup(func() { config.Env = old })
	config.Env = func(k string) string { return m[k] }
}

func TestTopic(t *testing.T) {
	if got := topic("site/{title}/{device}", "d1", "pump/7#"); got != "site/pump_7_/d1" {
		t.Errorf("topic = %s", got)
	}
	for _, bad := range []string{"", "a/+/b", "a/#", "a/{nope}"} {
		if checkTopic(bad) == nil {
			t.Errorf("checkTopic(%q) should fail", bad)
		}
	}
	if checkTopic(defaultTopic) != nil {
		t.Error("default topic should be fine")
	}
}

func TestLoadSettings(t *testing.T) {
	withEnv(t, map[string]string{"MQTT_BROKER": "tcp://broker:1883"})
	s, err := loadSettings()
	if err != nil || s.QoS != 1 || s.TLS != nil || s.Topic != defaultTopic {
		t.Fatalf("settings = %+v, err = %v", s, err)
	}
	withEnv(t, map[string]string{"MQTT_BROKER": "ssl://broker:8883", "MQTT_TLS_INSECURE": "true"})
	if s, err = loadSettings(); err != nil || s.TLS == nil || !s.TLS.InsecureSkipVerify {
		t.Fatalf("tls settings = %+v, err = %v", s, err)
	}
	for _, env := range []map[string]string{
		{},
		{"MQTT_BROKER": "http://broker"},
		{"MQTT_BROKER": "tcp://broker:1883", "MQTT_QOS": "3"},
		{"MQTT_BROKER": "ssl://broker:8883", "MQTT_TLS_CERT_FILE": "/x"},
		{"MQTT_BROKER": "ssl://broker:8883", "MQTT_TLS_CA_FILE": "/does/not/exist"},
	} {
		withEnv(t, env)
		if _, err := loadSettings(); err == nil {
			t.Errorf("want error for %v", env)
		}
	}
}
