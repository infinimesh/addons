package api

import (
	"testing"
	"time"

	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestParseApiUrl(t *testing.T) {
	cases := []struct {
		in     string
		target string
		plain  bool
	}{
		{"http://api:8000", "api:8000", true},
		{"https://api.example.com", "api.example.com:443", false},
		{"api.example.com:8443/", "api.example.com:8443", false},
		{"http://localhost", "localhost:80", true},
	}
	for _, c := range cases {
		target, plain := parseApiUrl(c.in)
		if target != c.target || plain != c.plain {
			t.Errorf("parseApiUrl(%q) = %q, %v; want %q, %v", c.in, target, plain, c.target, c.plain)
		}
	}
}

func TestShadowToState(t *testing.T) {
	data, _ := structpb.NewStruct(map[string]interface{}{"temp": 21.5})
	ts := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	got := ShadowToState(&shadowpb.Shadow{
		Device:   "d1",
		Reported: &shadowpb.State{Data: data, Timestamp: timestamppb.New(ts)},
	})
	if got == nil {
		t.Fatal("want state, got nil")
	}
	if v := got.Result.ReportedState.Data["temp"]; v != 21.5 {
		t.Errorf("temp = %v, want 21.5", v)
	}
	if got.Result.ReportedState.Timestamp != "2026-10-12T08:00:00Z" {
		t.Errorf("timestamp = %q", got.Result.ReportedState.Timestamp)
	}
	if ShadowToState(&shadowpb.Shadow{Device: "d1"}) != nil {
		t.Error("want nil for a shadow without reported state")
	}
}

// TestShadowToStateKeepsVersion: the reported version from core must reach
// the sinks (redisstream, elasticsearch, csvwriter, saphana).
func TestShadowToStateKeepsVersion(t *testing.T) {
	data, _ := structpb.NewStruct(map[string]interface{}{"temp": 1.0})
	got := ShadowToState(&shadowpb.Shadow{Device: "d1", Reported: &shadowpb.State{Data: data, Version: 6}})
	if got == nil || got.Result.ReportedState.Version != "6" {
		t.Fatalf("state = %+v, want version 6", got)
	}
	got = ShadowToState(&shadowpb.Shadow{Device: "d1", Reported: &shadowpb.State{Data: data}})
	if got.Result.ReportedState.Version != "" {
		t.Errorf("version = %q, want empty when core sends none", got.Result.ReportedState.Version)
	}
}
