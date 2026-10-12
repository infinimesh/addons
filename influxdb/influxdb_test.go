package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/message"
	ihttp "github.com/influxdata/influxdb-client-go/v2/api/http"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

func TestLoadSettings(t *testing.T) {
	old := config.Env
	t.Cleanup(func() { config.Env = old })
	config.Env = func(k string) string { return map[string]string{"INFLUX_URL": "http://x:8086"}[k] }
	if _, err := loadSettings(); err == nil {
		t.Error("want error without bucket")
	}
	config.Env = func(k string) string {
		return map[string]string{"INFLUX_URL": "http://x:8086", "INFLUX_BUCKET": "b"}[k]
	}
	s, err := loadSettings()
	if err != nil || s.Measurement != "device_state" || s.BatchSize != 1000 {
		t.Errorf("settings = %+v, err = %v", s, err)
	}
}

func TestPoint(t *testing.T) {
	m := &message.Message{Device: "d1", Title: "pump 7",
		Reported:   &message.State{Timestamp: "2026-10-12T08:00:00Z", Data: map[string]any{"temp": 20.5, "ok": true, "gps": map[string]any{"lat": 1.0}, "tags": []any{"a"}}},
		Desired:    &message.State{Data: map[string]any{"fan": "on"}},
		Connection: &message.Connection{Connected: true}}
	p, ok := point(&settings{Measurement: "m"}, m)
	if !ok {
		t.Fatal("no point")
	}
	line := write.PointToLineProtocol(p, 1)
	for _, want := range []string{`m,device=d1,title=pump\ 7 `, "temp=20.5", "ok=true", "gps.lat=1", `tags="[\"a\"]"`, "connected=true", " 1791792000000000000"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %s", want, line)
		}
	}
	if strings.Contains(line, "desired") {
		t.Errorf("desired should be off by default: %s", line)
	}
	p, _ = point(&settings{Measurement: "m", Desired: true}, m)
	if line := write.PointToLineProtocol(p, 1); !strings.Contains(line, `desired.fan="on"`) {
		t.Errorf("want desired.fan in %s", line)
	}
	if _, ok := point(&settings{}, &message.Message{Device: "d1", Desired: &message.State{Data: map[string]any{"x": 1.0}}}); ok {
		t.Error("desired only update should give no point by default")
	}
}

func TestRejected(t *testing.T) {
	for code, want := range map[int]bool{400: true, 422: true, 401: false, 429: false, 503: false} {
		err := fmt.Errorf("write: %w", &ihttp.Error{StatusCode: code})
		if rejected(err) != want {
			t.Errorf("rejected(%d) = %v", code, !want)
		}
	}
	if rejected(errors.New("dial")) {
		t.Error("network errors should be retried")
	}
}
