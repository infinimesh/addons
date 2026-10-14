package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/runner"
	"github.com/infinimesh/addons/sdk/runner/runnertest"
	shadowpb "github.com/infinimesh/proto/shadow"
)

type fakeConn struct {
	redis.Conn
	mu      *sync.Mutex
	entries *[]string
}

func (c fakeConn) Do(cmd string, args ...interface{}) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cmd == "XADD" {
		*c.entries = append(*c.entries, args[len(args)-3].(string)+" "+args[len(args)-1].(string))
	}
	return []byte("1-0"), nil
}
func (c fakeConn) Close() error { return nil }
func (c fakeConn) Err() error   { return nil }

func TestSinkOnNamespaceStream(t *testing.T) {
	var mu sync.Mutex
	var entries []string
	pool := &redis.Pool{Dial: func() (redis.Conn, error) { return fakeConn{mu: &mu, entries: &entries}, nil }}
	runnertest.Scenario(t, func(ctx context.Context, a api.Handler) {
		runner.Serve(ctx, a, sink(pool, 0), time.Hour, shadowpb.StateKey_REPORTED)
	}, []shadowpb.StateKey{shadowpb.StateKey_REPORTED})
	mu.Lock()
	defer mu.Unlock()
	if len(entries) != 2 || !strings.Contains(entries[0], `"uid":"d1"`) || !strings.Contains(entries[0], `"temp":1`) ||
		!strings.Contains(entries[1], `"uid":"d2"`) || !strings.Contains(entries[1], `"name":"pump"`) || !strings.Contains(entries[1], `"temp":2`) {
		t.Errorf("entries = %q, want d1 temp 1 and d2 temp 2 only", entries)
	}
}
