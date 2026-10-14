package wrappers

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
	devices []string
	open    map[string]int
}

func (f *fakeAPI) GetNamespaces() (*api.NamespacesRes, error) {
	return &api.NamespacesRes{Namespaces: []api.Namespace{{ID: "ns1"}}}, nil
}

func (f *fakeAPI) GetObjects(string) (*api.ObjectsRes, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := &api.ObjectsRes{}
	for _, d := range f.devices {
		res.Objects = append(res.Objects, api.Object{UID: d})
	}
	return res, nil
}

func (f *fakeAPI) GetDeviceShadowStream(ctx context.Context, id string) (<-chan *shadowpb.Shadow, error) {
	f.mu.Lock()
	f.open[id]++
	f.mu.Unlock()
	ch := make(chan *shadowpb.Shadow)
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		f.open[id]--
		f.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

func (f *fakeAPI) openStreams(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open[id]
}

// streamWorker follows the pattern of the addon workers: the stream context
// lives as long as Start.
type streamWorker struct {
	api  api.Handler
	obj  api.Object
	done chan struct{}
}

func (w *streamWorker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := w.api.GetDeviceShadowStream(ctx, w.obj.UID)
	for {
		select {
		case <-w.done:
			return
		case <-ch:
		}
	}
}

func (w *streamWorker) Stop() { close(w.done) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestRemovedDeviceStreamIsStopped checks that the stream of a device that
// left the namespace is stopped on the next refresh, and that Stop ends Start.
func TestRemovedDeviceStreamIsStopped(t *testing.T) {
	f := &fakeAPI{devices: []string{"d1", "d2"}, open: map[string]int{}}
	m := NewObjectManager(f, func(o api.Object) Process {
		return &streamWorker{api: f, obj: o, done: make(chan struct{})}
	}, time.Millisecond).(*objectManagerImpl)

	started := make(chan struct{})
	go func() {
		m.Start()
		close(started)
	}()
	waitFor(t, "both streams", func() bool { return f.openStreams("d1") == 1 && f.openStreams("d2") == 1 })

	f.mu.Lock()
	f.devices = []string{"d2"}
	f.mu.Unlock()
	waitFor(t, "d1 stream to stop", func() bool { return f.openStreams("d1") == 0 })
	if n := f.openStreams("d2"); n != 1 {
		t.Errorf("d2 open streams = %d, want 1", n)
	}

	m.Stop()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
	waitFor(t, "d2 stream to stop", func() bool { return f.openStreams("d2") == 0 })
}
