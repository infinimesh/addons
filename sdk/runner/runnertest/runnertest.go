// Package runnertest has a fake namespace stream for addon tests.
package runnertest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// API is an api.Handler with one namespace, "ns1" ("plant"), whose stream
// sends what the test passes to Send.
type API struct {
	api.Handler
	in chan *shadowpb.Shadow

	mu      sync.Mutex
	Include [][]shadowpb.StateKey
}

func New() *API { return &API{in: make(chan *shadowpb.Shadow)} }

func (f *API) GetNamespaces() (*api.NamespacesRes, error) {
	return &api.NamespacesRes{Namespaces: []api.Namespace{{ID: "ns1", Name: "plant"}}}, nil
}

func (f *API) StreamNamespace(ctx context.Context, _ string, include []shadowpb.StateKey) <-chan *shadowpb.Shadow {
	f.mu.Lock()
	f.Include = append(f.Include, include)
	f.mu.Unlock()
	out := make(chan *shadowpb.Shadow)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case s := <-f.in:
				select {
				case out <- s:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// Send hands s to the stream and waits until it was read.
func (f *API) Send(t *testing.T, s *shadowpb.Shadow) {
	t.Helper()
	select {
	case f.in <- s:
	case <-time.After(5 * time.Second):
		t.Fatal("stream not read")
	}
}

// Flush makes sure every message sent before it was fully handled.
func (f *API) Flush(t *testing.T) {
	t.Helper()
	f.Send(t, &shadowpb.Shadow{Device: "flush", Event: shadowpb.ShadowEvent_DEVICE_REMOVED})
	f.Send(t, &shadowpb.Shadow{Device: "flush", Event: shadowpb.ShadowEvent_DEVICE_REMOVED})
}

func reported(device string, temp float64) *shadowpb.Shadow {
	data, _ := structpb.NewStruct(map[string]interface{}{"temp": temp})
	return &shadowpb.Shadow{Device: device, Event: shadowpb.ShadowEvent_STATE, Reported: &shadowpb.State{
		Data: data, Timestamp: timestamppb.New(time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)),
	}}
}

// Scenario runs serve against a namespace where d1 reports temp 1, d2 is added
// and reports temp 2, d1 is removed and then reports temp 3. A sink on the
// namespace stream must write temp 1 for d1 and temp 2 for d2 and nothing for
// d1 after it was removed. serve must stream with include (nil for all).
func Scenario(t *testing.T, serve func(ctx context.Context, a api.Handler), include []shadowpb.StateKey) {
	t.Helper()
	f := New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		serve(ctx, f)
		close(done)
	}()
	f.Send(t, reported("d1", 1))
	f.Send(t, &shadowpb.Shadow{Device: "d2", Title: "pump", Event: shadowpb.ShadowEvent_DEVICE_ADDED})
	f.Send(t, reported("d2", 2))
	f.Send(t, &shadowpb.Shadow{Device: "d1", Event: shadowpb.ShadowEvent_DEVICE_REMOVED})
	f.Send(t, reported("d1", 3))
	f.Flush(t)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Include) != 1 {
		t.Fatalf("namespace streams = %d, want 1", len(f.Include))
	}
	if got := f.Include[0]; len(got) != len(include) || (len(got) > 0 && got[0] != include[0]) {
		t.Errorf("include = %v, want %v", got, include)
	}
}
