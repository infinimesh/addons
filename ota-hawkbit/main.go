package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
	"github.com/infinimesh/addons/sdk/api"
	pb "github.com/infinimesh/proto/node"
)

type staticToken struct{}

func (staticToken) GetToken() string    { return "" }
func (staticToken) RefreshToken() error { return nil }

func main() {
	settings, err := otacore.FromEnv(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	hbURL := strings.TrimSpace(os.Getenv("HAWKBIT_URL"))
	if err := settings.AllowURL(hbURL); err != nil {
		log.Fatal(err)
	}
	c, err := newClient(hbURL, os.Getenv("HAWKBIT_USER"), os.Getenv("HAWKBIT_PASSWORD"), split(os.Getenv("OTA_GROUP_TAGS")))
	if err != nil {
		log.Fatal(err)
	}
	poll, err := pollInterval(os.Getenv("OTA_POLL_SECS"))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := api.Dial(settings.APIURL)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	handler := api.NewHandler(conn, staticToken{})
	err = (&otacore.Runner{
		Settings: settings,
		Backend:  &backend{client: c},
		Open:     otacore.OpenNamespace(handler, settings.Token),
		Writer:   otacore.NewWriter(pb.NewShadowServiceClient(conn), settings.Token),
		Status:   ActionStatus,
		Poll:     poll,
	}).Run(context.Background())
	if err != nil {
		log.Fatal(err)
	}
}

func pollInterval(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 15 * time.Second, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.New("OTA_POLL_SECS must be a whole number of seconds")
	}
	return time.Duration(n) * time.Second, nil
}

func split(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.Split(v, ",")
}
