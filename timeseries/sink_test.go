package main

import (
	"context"
	"sync"
	"testing"
	"time"

	redistimeseries "github.com/RedisTimeSeries/redistimeseries-go"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/runner"
	"github.com/infinimesh/addons/sdk/runner/runnertest"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeSeries struct {
	mu       sync.Mutex
	created  []string
	added    []point
	labels   map[string]string
	times    []int64
	policies []redistimeseries.DuplicatePolicyType
}

func (f *fakeSeries) CreateKeyWithOptions(key string, o redistimeseries.CreateOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, key)
	f.labels = o.Labels
	return nil
}

func (f *fakeSeries) AddWithOptions(key string, ts int64, v float64, o redistimeseries.CreateOptions) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, point{key, v})
	f.times = append(f.times, ts)
	f.policies = append(f.policies, o.DuplicatePolicy)
	return ts, nil
}

func TestSinkOnNamespaceStream(t *testing.T) {
	f := &fakeSeries{}
	runnertest.Scenario(t, func(ctx context.Context, a api.Handler) {
		runner.Serve(ctx, a, newSink(f).handle, time.Hour, shadowpb.StateKey_REPORTED)
	}, []shadowpb.StateKey{shadowpb.StateKey_REPORTED})
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.added) != 2 || f.added[0] != (point{"temp:d1", 1}) || f.added[1] != (point{"temp:d2", 2}) {
		t.Errorf("samples = %v, want temp:d1 1 and temp:d2 2 only", f.added)
	}
	if len(f.created) != 2 || f.labels["name"] != "pump" || f.labels["uid"] != "d2" {
		t.Errorf("created = %v labels = %v", f.created, f.labels)
	}
}

// TestReplayKeepsUpdateTimes covers the reconnect replay: updates arrive in
// one burst and must keep their own reported times, with duplicate policy
// LAST, so RedisTimeSeries does not drop them.
func TestReplayKeepsUpdateTimes(t *testing.T) {
	f := &fakeSeries{}
	k := newSink(f)
	base := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	for i := range 26 {
		data, _ := structpb.NewStruct(map[string]interface{}{"temp": float64(i)})
		k.handle(api.Object{UID: "d1"}, &shadowpb.Shadow{Device: "d1", Reported: &shadowpb.State{
			Data: data, Timestamp: timestamppb.New(base.Add(time.Duration(i) * time.Second)),
		}})
	}
	if len(f.times) != 26 {
		t.Fatalf("samples = %d, want 26", len(f.times))
	}
	seen := map[int64]bool{}
	for i, ts := range f.times {
		if want := base.Add(time.Duration(i) * time.Second).UnixMilli(); ts != want {
			t.Errorf("sample %d time = %d, want %d", i, ts, want)
		}
		seen[ts] = true
		if f.policies[i] != redistimeseries.LastDuplicatePolicy {
			t.Errorf("sample %d duplicate policy = %q, want last", i, f.policies[i])
		}
	}
	if len(seen) != 26 {
		t.Errorf("distinct times = %d, want 26", len(seen))
	}
}
