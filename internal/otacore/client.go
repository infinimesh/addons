package otacore

import (
	"context"

	"github.com/infinimesh/addons/sdk/api"
	pb "github.com/infinimesh/proto/node"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc/metadata"
)

// OpenNamespace streams one namespace with the addon token. The resume and
// sync loop is StreamNamespaceToken.
func OpenNamespace(h api.Handler, token string) StreamOpen {
	return func(ctx context.Context, namespace string) (<-chan *shadowpb.Shadow, <-chan error) {
		return h.StreamNamespaceToken(ctx, namespace, token, nil)
	}
}

type grpcWriter struct {
	client pb.ShadowServiceClient
	token  string
}

// NewWriter patches device state with the addon token.
func NewWriter(client pb.ShadowServiceClient, token string) Writer {
	return &grpcWriter{client: client, token: token}
}

func (g *grpcWriter) Patch(ctx context.Context, p Patch) error {
	sh, err := shadowFrom(p)
	if err != nil {
		return err
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+g.token)
	_, err = g.client.Patch(ctx, sh)
	if err != nil {
		return asCall(err)
	}
	return nil
}
