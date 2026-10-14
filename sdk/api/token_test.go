package api

import (
	"context"
	"testing"
	"time"

	pb "github.com/infinimesh/proto/node"
	"google.golang.org/grpc"
)

type tokenConn struct {
	grpc.ClientConnInterface
	req *pb.TokenRequest
}

func (c *tokenConn) Invoke(_ context.Context, _ string, args, reply any, _ ...grpc.CallOption) error {
	c.req = args.(*pb.TokenRequest)
	reply.(*pb.TokenResponse).Token = "t"
	return nil
}

func TestNewTokenHandlerSetsExpiry(t *testing.T) {
	c := &tokenConn{}
	before := time.Now()
	h := NewTokenHandler(c, "u", "p", time.Hour)
	got := time.Unix(c.req.GetExpiresAt(), 0)
	if got.Before(before.Add(2*time.Hour-time.Second)) || got.After(time.Now().Add(2*time.Hour)) {
		t.Fatalf("expires_at = %v, want 2h from now", got)
	}
	if h.GetToken() != "t" {
		t.Fatalf("token = %q", h.GetToken())
	}
}
