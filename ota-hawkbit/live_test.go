//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
)

func TestLiveAssignOnce(t *testing.T) {
	base := os.Getenv("HAWKBIT_URL")
	if base == "" {
		base = "http://127.0.0.1:18080"
	}
	user := os.Getenv("HAWKBIT_USER")
	if user == "" {
		user = "admin"
	}
	pass := os.Getenv("HAWKBIT_PASSWORD")
	if pass == "" {
		pass = "admin"
	}
	c, err := newClient(base, user, pass, []string{"canary"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := waitReady(ctx, c); err != nil {
		t.Fatal(err)
	}
	version := "it-" + time.Now().UTC().Format("20060102150405")
	if err := createDist(ctx, c, version); err != nil {
		t.Fatal(err)
	}
	b := &backend{client: c}
	d := otacore.Device{ID: "it-" + version, Namespace: "plant", Title: "pump", Tags: []string{"canary", "other"}}
	if err := b.EnsureDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := b.SyncGroup(ctx, d, d.Tags, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Deploy(ctx, d, version); err != nil {
		t.Fatal(err)
	}
	if err := b.Deploy(ctx, d, version); err != nil {
		t.Fatal(err)
	}
	n, err := actionCount(ctx, c, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("actions = %d, want 1", n)
	}
	err = b.Deploy(ctx, d, version+"-missing")
	var skip *otacore.SkipDeploy
	if !errors.As(err, &skip) {
		t.Fatalf("missing version err = %v", err)
	}
	snap, err := b.Status(ctx, d)
	if err != nil || snap.Version != version {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
	if err := b.Forget(ctx, d); err != nil {
		t.Fatal(err)
	}
}

func waitReady(ctx context.Context, c *client) error {
	var last error
	for {
		code, _, err := c.do(ctx, http.MethodGet, "/rest/v1/distributionsettypes?limit=1", nil)
		if err == nil && code == http.StatusOK {
			return nil
		}
		if err != nil {
			last = err
		} else {
			last = errors.New("hawkbit status " + http.StatusText(code))
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(2 * time.Second):
		}
	}
}

func createDist(ctx context.Context, c *client, version string) error {
	code, raw, err := c.do(ctx, http.MethodPost, "/rest/v1/softwaremodules", []map[string]any{{
		"vendor":  "infinimesh",
		"name":    version,
		"version": version,
		"type":    "os",
	}})
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return errors.New("software module " + string(raw))
	}
	rows, err := decodeList(raw)
	if err != nil || len(rows) == 0 {
		return errors.New("software module response " + string(raw))
	}
	code, raw, err = c.do(ctx, http.MethodPost, "/rest/v1/distributionsets", []map[string]any{{
		"name":                  version,
		"version":               version,
		"type":                  "os",
		"requiredMigrationStep": false,
		"modules":               []map[string]any{{"id": num(rows[0]["id"])}},
	}})
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return errors.New("distribution set " + string(raw))
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	return nil
}

func actionCount(ctx context.Context, c *client, controller string) (int, error) {
	code, raw, err := c.do(ctx, http.MethodGet, "/rest/v1/targets/"+controller+"/actions?limit=50", nil)
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, errors.New("actions " + string(raw))
	}
	rows, err := decodeList(raw)
	return len(rows), err
}
