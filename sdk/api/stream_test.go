package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	pb "github.com/infinimesh/proto/node"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeTokens struct{}

func (fakeTokens) RefreshToken() error { return nil }
func (fakeTokens) GetToken() string    { return "account" }

type fakeDevices struct {
	pb.DevicesServiceClient
	mu   sync.Mutex
	reqs []*pb.DevicesTokenRequest
}

func (f *fakeDevices) MakeDevicesToken(_ context.Context, in *pb.DevicesTokenRequest, _ ...grpc.CallOption) (*pb.TokenResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, in)
	return &pb.TokenResponse{Token: "stream"}, nil
}

type fakeShadow struct {
	pb.ShadowServiceClient
	mu      sync.Mutex
	reqs    []*shadowpb.StreamShadowRequest
	auth    []string
	streams [][]*shadowpb.Shadow
	open    int
}

func (f *fakeShadow) StreamShadow(ctx context.Context, in *shadowpb.StreamShadowRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[shadowpb.Shadow], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	md, _ := metadata.FromOutgoingContext(ctx)
	f.auth = append(f.auth, md.Get("authorization")...)
	f.reqs = append(f.reqs, in)
	var msgs []*shadowpb.Shadow
	if len(f.streams) > 0 {
		msgs, f.streams = f.streams[0], f.streams[1:]
	}
	f.open++
	return &fakeStream{ctx: ctx, msgs: msgs, f: f}, nil
}

func (f *fakeShadow) openStreams() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open
}

type fakeStream struct {
	grpc.ClientStream
	ctx    context.Context
	msgs   []*shadowpb.Shadow
	f      *fakeShadow
	closed bool
}

func (s *fakeStream) Recv() (*shadowpb.Shadow, error) {
	if len(s.msgs) > 0 {
		m := s.msgs[0]
		s.msgs = s.msgs[1:]
		return m, nil
	}
	if s.msgs != nil {
		s.msgs = nil
		s.done()
		return nil, errors.New("connection reset")
	}
	<-s.ctx.Done()
	s.done()
	return nil, io.EOF
}

func (s *fakeStream) done() {
	if s.closed {
		return
	}
	s.closed = true
	s.f.mu.Lock()
	s.f.open--
	s.f.mu.Unlock()
}

func recv(t *testing.T, ch <-chan *shadowpb.Shadow) *shadowpb.Shadow {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
		return nil
	}
}

func TestStreamNamespaceResumesAfterReconnect(t *testing.T) {
	dev := &fakeDevices{}
	sh := &fakeShadow{streams: [][]*shadowpb.Shadow{
		{
			{Device: "d1", Event: shadowpb.ShadowEvent_DEVICE_ADDED, Sequence: 1},
			{Device: "d1", Sequence: 7},
		},
		{
			{Device: "d1", Sequence: 8},
		},
	}}
	h := &handlerImpl{tokenHandler: fakeTokens{}, devices: dev, shadow: sh}
	ctx, cancel := context.WithCancel(context.Background())
	include := []shadowpb.StateKey{shadowpb.StateKey_REPORTED}
	ch := h.StreamNamespace(ctx, "ns1", include)

	var seqs []uint64
	for range 3 {
		seqs = append(seqs, recv(t, ch).GetSequence())
	}
	if seqs[0] != 1 || seqs[1] != 7 || seqs[2] != 8 {
		t.Fatalf("sequences = %v, want [1 7 8]", seqs)
	}
	cancel()
	for range ch {
	}

	if n := sh.openStreams(); n != 0 {
		t.Errorf("open streams after cancel = %d, want 0", n)
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if len(sh.reqs) < 2 {
		t.Fatalf("StreamShadow calls = %d, want at least 2", len(sh.reqs))
	}
	for i, want := range []uint64{0, 7} {
		r := sh.reqs[i]
		if r.GetNamespace() != "ns1" || len(r.GetDevices()) != 0 || r.GetResumeAfter() != want {
			t.Errorf("request %d = %v, want namespace ns1, no devices, resume_after %d", i, r, want)
		}
		if len(r.GetInclude()) != 1 || r.GetInclude()[0] != shadowpb.StateKey_REPORTED {
			t.Errorf("request %d include = %v", i, r.GetInclude())
		}
		if sh.auth[i] != "Bearer stream" {
			t.Errorf("request %d authorization = %q, want the stream token", i, sh.auth[i])
		}
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	for _, r := range dev.reqs[:2] {
		if !r.GetStream() || r.GetNamespace() != "ns1" || len(r.GetDevices()) != 0 || r.GetExpiresAt() == 0 {
			t.Errorf("token request = %v, want a namespace stream token with an expiry", r)
		}
	}
}

type refuseDevices struct {
	pb.DevicesServiceClient
	called bool
}

func (f *refuseDevices) MakeDevicesToken(context.Context, *pb.DevicesTokenRequest, ...grpc.CallOption) (*pb.TokenResponse, error) {
	f.called = true
	return nil, errors.New("addon token must not mint a devices token")
}

func TestAddonStreamSyncsUntilItHasAResumePoint(t *testing.T) {
	sh := &fakeShadow{streams: [][]*shadowpb.Shadow{{{Device: "d", Sequence: 3}}}}
	h := &handlerImpl{tokenHandler: fakeTokens{}, devices: &fakeDevices{}, shadow: sh, retry: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := h.StreamNamespaceToken(ctx, "ns1", "tok", nil, 0)
	if recv(t, ch).GetSequence() != 3 {
		t.Fatal("sequence")
	}
	cancel()
	for range ch {
	}
	sh.mu.Lock()
	if len(sh.reqs) != 1 || !sh.reqs[0].GetSync() || sh.reqs[0].GetResumeAfter() != 0 {
		t.Fatalf("first = sync %v resume %d", sh.reqs[0].GetSync(), sh.reqs[0].GetResumeAfter())
	}
	sh.mu.Unlock()

	sh = &fakeShadow{streams: [][]*shadowpb.Shadow{{{Device: "d", Sequence: 11}}}}
	h = &handlerImpl{tokenHandler: fakeTokens{}, devices: &fakeDevices{}, shadow: sh, retry: time.Hour}
	ctx, cancel = context.WithCancel(context.Background())
	ch, _ = h.StreamNamespaceToken(ctx, "ns1", "tok", nil, 9)
	if recv(t, ch).GetSequence() != 11 {
		t.Fatal("resumed sequence")
	}
	cancel()
	for range ch {
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if len(sh.reqs) != 1 || sh.reqs[0].GetSync() || sh.reqs[0].GetResumeAfter() != 9 {
		t.Fatalf("resume = sync %v resume %d", sh.reqs[0].GetSync(), sh.reqs[0].GetResumeAfter())
	}
}

func TestStreamNamespaceTokenUsesAddonBearer(t *testing.T) {
	dev := &refuseDevices{}
	sh := &gapShadow{scripts: []gapScript{
		{msgs: []*shadowpb.Shadow{{Device: "d1", Sequence: 4}}, err: status.Error(codes.Unauthenticated, "token is revoked")},
	}}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	h := &handlerImpl{tokenHandler: fakeTokens{}, devices: dev, shadow: sh, retry: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, errc := h.StreamNamespaceToken(ctx, "ns1", "addon-secret", nil, 0)
	got := recv(t, ch)
	if got.GetDevice() != "d1" {
		t.Fatalf("device = %s", got.GetDevice())
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("stream stayed open after token is revoked")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop")
	}
	select {
	case err := <-errc:
		if !errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revocation was not reported")
	}
	if dev.called {
		t.Fatal("MakeDevicesToken was called")
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if len(sh.reqs) != 1 {
		t.Fatalf("StreamShadow calls = %d, want 1", len(sh.reqs))
	}
	if !bytes.Contains(buf.Bytes(), []byte("token is revoked; run inf plugins rotate and update the secret")) {
		t.Fatalf("log = %q", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("addon-secret")) {
		t.Fatal("log contains the addon token")
	}
}
