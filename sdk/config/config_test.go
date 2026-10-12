package config

import (
	"testing"
	"time"
)

func withEnv(t *testing.T, m map[string]string) {
	old := Env
	Env = func(k string) string { return m[k] }
	t.Cleanup(func() { Env = old })
}

func TestDeviceRefreshNames(t *testing.T) {
	withEnv(t, map[string]string{})
	if d, _ := DeviceRefresh(); d != 10*time.Minute {
		t.Errorf("default = %v", d)
	}
	withEnv(t, map[string]string{"OBJECT_REFRESH_INTERVAL_SECS": "30"})
	if d, _ := DeviceRefresh(); d != 30*time.Second {
		t.Errorf("old name = %v", d)
	}
	withEnv(t, map[string]string{"OBJECT_REFRESH_INTERVAL_SECS": "30", "DEVICE_REFRESH_INTERVAL_SECS": "20"})
	if d, _ := DeviceRefresh(); d != 20*time.Second {
		t.Errorf("new name should win, got %v", d)
	}
	withEnv(t, map[string]string{"TOKEN_REFRESH_INTERVAL_SECS": "abc"})
	if _, err := TokenRefresh(); err == nil {
		t.Error("want error for abc")
	}
}

func TestRedisOptions(t *testing.T) {
	withEnv(t, map[string]string{"REDIS_PASSWORD": "x", "REDIS_TLS": "true"})
	opts, err := RedisDialOptions()
	if err != nil || len(opts) != 4 {
		t.Fatalf("opts = %d, err = %v", len(opts), err)
	}
	withEnv(t, map[string]string{"REDIS_TLS": "maybe"})
	if _, err := RedisDialOptions(); err == nil {
		t.Error("want error for maybe")
	}
	withEnv(t, map[string]string{})
	if _, err := RedisPool(); err == nil {
		t.Error("want error without REDIS_ADDR")
	}
}
