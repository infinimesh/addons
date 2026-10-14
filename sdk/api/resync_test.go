package api

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	pb "github.com/infinimesh/proto/node"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// gapShadow serves scripted streams: each sends its messages and then ends
// with its error (or blocks until cancel when the error is nil).
type gapShadow struct {
	pb.ShadowServiceClient
	mu      sync.Mutex
	reqs    []*shadowpb.StreamShadowRequest
	scripts []gapScript
}

type gapScript struct {
	msgs []*shadowpb.Shadow
	err  error
}

func (f *gapShadow) StreamShadow(ctx context.Context, in *shadowpb.StreamShadowRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[shadowpb.Shadow], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, in)
	var sc gapScript
	if len(f.scripts) > 0 {
		sc, f.scripts = f.scripts[0], f.scripts[1:]
	}
	return &gapStream{ctx: ctx, sc: sc}, nil
}

type gapStream struct {
	grpc.ClientStream
	ctx context.Context
	sc  gapScript
}

func (s *gapStream) Recv() (*shadowpb.Shadow, error) {
	if len(s.sc.msgs) > 0 {
		m := s.sc.msgs[0]
		s.sc.msgs = s.sc.msgs[1:]
		return m, nil
	}
	if s.sc.err != nil {
		return nil, s.sc.err
	}
	<-s.ctx.Done()
	return nil, io.EOF
}

// TestStreamNamespaceResyncsAfterGap: when core closes the stream because it
// fell behind or the resume window was exceeded, the next open must ask for
// sync (not a resume_after core can no longer replay). A normal error keeps
// resume_after. Core sends Aborted, or Unknown with the Aborted text.
func TestStreamNamespaceResyncsAfterGap(t *testing.T) {
	cases := []struct {
		name string
		err  error
		sync bool
	}{
		{"aborted code", status.Error(codes.Aborted, "shadow stream fell behind"), true},
		{"unknown with fell behind text", status.Error(codes.Unknown, "rpc error: code = Aborted desc = shadow stream fell behind"), true},
		{"resume window exceeded", status.Error(codes.OutOfRange, "resume window exceeded"), true},
		{"other error", errors.New("connection reset"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sh := &gapShadow{scripts: []gapScript{
				{msgs: []*shadowpb.Shadow{{Device: "d1", Sequence: 5}}, err: c.err},
				{msgs: []*shadowpb.Shadow{{Device: "d1", Sequence: 900}}},
			}}
			h := &handlerImpl{tokenHandler: fakeTokens{}, devices: &fakeDevices{}, shadow: sh}
			ctx, cancel := context.WithCancel(context.Background())
			ch := h.StreamNamespace(ctx, "ns1", nil)
			if s := recv(t, ch).GetSequence(); s != 5 {
				t.Fatalf("first sequence = %d", s)
			}
			if s := recv(t, ch).GetSequence(); s != 900 {
				t.Fatalf("second sequence = %d", s)
			}
			cancel()
			for range ch {
			}
			sh.mu.Lock()
			defer sh.mu.Unlock()
			if len(sh.reqs) < 2 {
				t.Fatalf("opens = %d, want at least 2", len(sh.reqs))
			}
			if sh.reqs[0].GetSync() {
				t.Error("first open asked for sync")
			}
			r := sh.reqs[1]
			if c.sync && (!r.GetSync() || r.GetResumeAfter() != 0) {
				t.Errorf("reopen = sync %v resume_after %d, want sync and no resume_after", r.GetSync(), r.GetResumeAfter())
			}
			if !c.sync && (r.GetSync() || r.GetResumeAfter() != 5) {
				t.Errorf("reopen = sync %v resume_after %d, want resume_after 5 without sync", r.GetSync(), r.GetResumeAfter())
			}
		})
	}
}
