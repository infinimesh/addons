package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/runner"
	"github.com/infinimesh/addons/sdk/runner/runnertest"
	"github.com/twmb/franz-go/pkg/kgo"
)

type fakeProducer struct {
	mu   sync.Mutex
	recs []string
}

func (p *fakeProducer) Produce(_ context.Context, r *kgo.Record, promise func(*kgo.Record, error)) {
	p.mu.Lock()
	p.recs = append(p.recs, string(r.Key)+" "+string(r.Value))
	p.mu.Unlock()
	promise(r, nil)
}

func TestSinkOnNamespaceStream(t *testing.T) {
	p := &fakeProducer{}
	runnertest.Scenario(t, func(ctx context.Context, a api.Handler) {
		runner.Serve(ctx, a, sink(p, true), time.Hour)
	}, nil)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.recs) != 2 || !strings.HasPrefix(p.recs[0], "d1 ") || !strings.Contains(p.recs[0], `"temp":1`) ||
		!strings.HasPrefix(p.recs[1], "d2 ") || !strings.Contains(p.recs[1], `"temp":2`) {
		t.Errorf("records = %q, want d1 temp 1 and d2 temp 2 only", p.recs)
	}
}
