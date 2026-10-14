package api

import (
	"context"
	"runtime"
	"testing"
	"time"

	shadowpb "github.com/infinimesh/proto/shadow"
)

// waitGoroutines waits until the goroutine count is back to at most n.
func waitGoroutines(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > n {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines = %d, want at most %d\n%s", runtime.NumGoroutine(), n, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestDeviceStreamsStopOnCancel opens many device streams, cancels them, and
// checks that every StreamShadow call and every goroutine is gone.
func TestDeviceStreamsStopOnCancel(t *testing.T) {
	before := runtime.NumGoroutine()
	sh := &fakeShadow{}
	h := &handlerImpl{tokenHandler: fakeTokens{}, devices: &fakeDevices{}, shadow: sh, retry: time.Millisecond}

	var cancels []context.CancelFunc
	var shadows []<-chan *shadowpb.Shadow
	var states []<-chan *DevicesStateRes
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		ch, err := h.GetDeviceShadowStream(ctx, "d1")
		if err != nil {
			t.Fatal(err)
		}
		shadows = append(shadows, ch)
		ctx2, cancel2 := context.WithCancel(context.Background())
		cancels = append(cancels, cancel2)
		st, err := h.GetDevicesStateStream(ctx2, "d1")
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, st)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sh.openStreams() < 100 {
		if time.Now().After(deadline) {
			t.Fatalf("open streams = %d, want 100", sh.openStreams())
		}
		time.Sleep(time.Millisecond)
	}
	for _, c := range cancels {
		c()
	}
	for _, ch := range shadows {
		for range ch {
		}
	}
	for _, ch := range states {
		for range ch {
		}
	}
	if n := sh.openStreams(); n != 0 {
		t.Errorf("open streams after cancel = %d, want 0", n)
	}
	waitGoroutines(t, before)
}
