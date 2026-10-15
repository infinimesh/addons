package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/infinimesh/addons/sdk/api"
	pb "github.com/infinimesh/proto/node"
	nspb "github.com/infinimesh/proto/node/namespaces"
	"google.golang.org/grpc/metadata"
)

func checkURLs(ctx context.Context, urls map[string]string, lookup func(context.Context, string) (string, error)) error {
	names := make([]string, 0, len(urls))
	for ns := range urls {
		names = append(names, ns)
	}
	sort.Strings(names)
	for _, ns := range names {
		got, err := lookup(ctx, ns)
		if err != nil {
			return fmt.Errorf("namespace %s: %w", ns, err)
		}
		if strings.TrimSpace(got) != strings.TrimSpace(urls[ns]) {
			return fmt.Errorf("namespace %s: OTA_WEBHOOK_URL does not match the namespace webhook_url", ns)
		}
	}
	return nil
}

type coreURLs struct {
	client pb.NamespacesServiceClient
	token  string
}

func (c coreURLs) WebhookURL(ctx context.Context, namespace string) (string, error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
	res, err := c.client.Get(ctx, &nspb.Namespace{Uuid: namespace})
	if err != nil {
		return "", readURLErr(err)
	}
	ota := res.GetOta()
	if ota.GetProvider() != "webhook" {
		return "", errors.New("ota provider does not match the namespace")
	}
	if strings.TrimSpace(ota.GetWebhookUrl()) == "" {
		return "", errors.New("webhook_url is required when provider is webhook")
	}
	return ota.GetWebhookUrl(), nil
}

func readURLErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "token is revoked"):
		return api.ErrTokenRevoked
	case strings.Contains(msg, "ota provider does not match the namespace"):
		return errors.New("ota provider does not match the namespace")
	case strings.Contains(msg, "ota provider requires the addon token"):
		return errors.New("ota provider requires the addon token")
	case strings.Contains(msg, "webhook_url is required when provider is webhook"):
		return errors.New("webhook_url is required when provider is webhook")
	default:
		return errors.New("could not read the namespace webhook_url")
	}
}
