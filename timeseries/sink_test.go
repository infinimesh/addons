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
)

type fakeSeries struct {
	mu      sync.Mutex
	created []string
	added   []point
	labels  map[string]string
}

func (f *fakeSeries) CreateKeyWithOptions(key string, o redistimeseries.CreateOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, key)
	f.labels = o.Labels
	return nil
}

func (f *fakeSeries) AddAutoTsWithOptions(key string, v float64, _ redistimeseries.CreateOptions) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, point{key, v})
	return 0, nil
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
