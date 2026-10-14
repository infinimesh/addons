package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	shadowpb "github.com/infinimesh/proto/shadow"
)

type fakeAPI struct {
	api.Handler
	mu      sync.Mutex
	streams map[string]chan *shadowpb.Shadow
	opened  []string
}

func (f *fakeAPI) GetNamespaces() (*api.NamespacesRes, error) {
	return &api.NamespacesRes{Namespaces: []api.Namespace{{ID: "ns1", Name: "one"}}}, nil
}

func (f *fakeAPI) StreamNamespace(ctx context.Context, ns string, _ []shadowpb.StateKey) <-chan *shadowpb.Shadow {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, ns)
	in := f.streams[ns]
	out := make(chan *shadowpb.Shadow)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case s := <-in:
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

// TestServePicksUpAddedAndRemovedDevices checks that one stream is opened per
// namespace and that a device added or removed while running is handled
// without a restart.
func TestServePicksUpAddedAndRemovedDevices(t *testing.T) {
	in := make(chan *shadowpb.Shadow)
	f := &fakeAPI{streams: map[string]chan *shadowpb.Shadow{"ns1": in}}
	got := make(chan string, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Serve(ctx, f, func(obj api.Object, s *shadowpb.Shadow) {
			got <- obj.UID + "/" + obj.Name + "/" + obj.NamespaceName
		}, time.Hour)
		close(done)
	}()

	send := func(s *shadowpb.Shadow) {
		select {
		case in <- s:
		case <-time.After(5 * time.Second):
			t.Fatal("stream not read")
		}
	}
	send(&shadowpb.Shadow{Device: "d1", Title: "first", Event: shadowpb.ShadowEvent_STATE, Sequence: 1})
	send(&shadowpb.Shadow{Device: "d2", Title: "added", Event: shadowpb.ShadowEvent_DEVICE_ADDED, Sequence: 2})
	send(&shadowpb.Shadow{Device: "d2", Event: shadowpb.ShadowEvent_STATE, Sequence: 3})
	send(&shadowpb.Shadow{Device: "d1", Event: shadowpb.ShadowEvent_DEVICE_REMOVED, Sequence: 4})
	send(&shadowpb.Shadow{Device: "d1", Event: shadowpb.ShadowEvent_STATE, Sequence: 5})
	send(&shadowpb.Shadow{Device: "d2", Event: shadowpb.ShadowEvent_STATE, Sequence: 6})
	// One more message makes sure the one before it was fully handled.
	send(&shadowpb.Shadow{Device: "d3", Event: shadowpb.ShadowEvent_DEVICE_REMOVED, Sequence: 7})
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	close(got)
	var calls []string
	for c := range got {
		calls = append(calls, c)
	}
	want := []string{"d1/first/one", "d2/added/one", "d2/added/one"}
	if len(calls) != len(want) {
		t.Fatalf("handler calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("handler calls = %v, want %v", calls, want)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opened) != 1 || f.opened[0] != "ns1" {
		t.Errorf("streams opened = %v, want one for ns1", f.opened)
	}
}
