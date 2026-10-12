package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/message"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	ihttp "github.com/influxdata/influxdb-client-go/v2/api/http"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

type settings struct {
	URL, Token, Org, Bucket, Measurement string
	Desired                              bool
	BatchSize                            int
	Flush                                time.Duration
	MaxBuffer                            int
	Timeout                              time.Duration
	TLSInsecure                          bool
}

func loadSettings() (*settings, error) {
	s := &settings{
		URL:         config.Env("INFLUX_URL"),
		Token:       config.Env("INFLUX_TOKEN"),
		Org:         config.Env("INFLUX_ORG"),
		Bucket:      config.Env("INFLUX_BUCKET"),
		Measurement: config.String("INFLUX_MEASUREMENT", "device_state"),
	}
	if s.URL == "" || s.Bucket == "" {
		return nil, fmt.Errorf("INFLUX_URL and INFLUX_BUCKET are required")
	}
	var err error
	if s.Desired, err = config.Bool("INFLUX_INCLUDE_DESIRED", false); err != nil {
		return nil, err
	}
	if s.BatchSize, err = config.Int("INFLUX_BATCH_SIZE", 1000); err != nil {
		return nil, err
	}
	if s.Flush, err = config.Seconds(time.Second, "INFLUX_FLUSH_INTERVAL_SECS"); err != nil {
		return nil, err
	}
	if s.MaxBuffer, err = config.Int("INFLUX_MAX_BUFFER", 100000); err != nil {
		return nil, err
	}
	if s.Timeout, err = config.Seconds(20*time.Second, "INFLUX_TIMEOUT_SECS"); err != nil {
		return nil, err
	}
	if s.TLSInsecure, err = config.Bool("INFLUX_TLS_INSECURE", false); err != nil {
		return nil, err
	}
	if s.BatchSize < 1 || s.MaxBuffer < s.BatchSize {
		return nil, fmt.Errorf("INFLUX_BATCH_SIZE must be at least 1 and INFLUX_MAX_BUFFER at least the batch size")
	}
	return s, nil
}

// flatten adds every value of data as a field. Nested objects become
// "parent.child". Numbers are floats, so a field keeps one type. Lists and
// nulls are stored as JSON strings.
func flatten(fields map[string]any, prefix string, data map[string]any) {
	for k, v := range data {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			flatten(fields, name, t)
		case float64, bool, string:
			fields[name] = t
		default:
			b, _ := json.Marshal(t)
			fields[name] = string(b)
		}
	}
}

// point turns a message into one point. ok is false when it has no fields.
func point(s *settings, m *message.Message) (p *write.Point, ok bool) {
	fields := map[string]any{}
	if m.Reported != nil {
		flatten(fields, "", m.Reported.Data)
	}
	if s.Desired && m.Desired != nil {
		flatten(fields, "desired", m.Desired.Data)
	}
	if m.Connection != nil {
		fields["connected"] = m.Connection.Connected
	}
	if len(fields) == 0 {
		return nil, false
	}
	tags := map[string]string{"device": m.Device}
	if t := strings.TrimSpace(m.Title); t != "" {
		tags["title"] = t
	}
	return influxdb2.NewPoint(s.Measurement, tags, fields, m.Time()), true
}

// rejected tells if InfluxDB refused the data itself, for example a field
// type conflict. Sending the same points again would fail again.
func rejected(err error) bool {
	var e *ihttp.Error
	if !errors.As(err, &e) {
		return false
	}
	return e.StatusCode >= 400 && e.StatusCode < 500 &&
		e.StatusCode != http.StatusTooManyRequests && e.StatusCode != http.StatusRequestTimeout &&
		e.StatusCode != http.StatusUnauthorized && e.StatusCode != http.StatusForbidden
}
