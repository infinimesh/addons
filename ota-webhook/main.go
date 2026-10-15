package main

import (
	"context"
	"log"
	"os"

	"github.com/infinimesh/addons/internal/otacore"
	"github.com/infinimesh/addons/sdk/api"
	pb "github.com/infinimesh/proto/node"
)

type staticToken struct{}

func (staticToken) GetToken() string    { return "" }
func (staticToken) RefreshToken() error { return nil }

func main() {
	cfg, err := load(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	book, err := loadBook(cfg.StateFile)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := api.Dial(cfg.APIURL)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	lookup := coreURLs{client: pb.NewNamespacesServiceClient(conn), token: cfg.Token}.WebhookURL
	if err = checkURLs(context.Background(), cfg.URLs, lookup); err != nil {
		log.Fatal(err)
	}
	handler := api.NewHandler(conn, staticToken{})
	running := newApp(cfg, book, openStreams(handler, cfg.Token), otacore.NewWriter(pb.NewShadowServiceClient(conn), cfg.Token))
	running.lookup = lookup
	err = running.Run(context.Background())
	if err != nil {
		log.Fatal(err)
	}
}
