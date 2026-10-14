// Package runner holds the common main loop of an addon that streams device
// shadows from the infinimesh API.
package runner

import (
	"context"
	"log"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	shadowpb "github.com/infinimesh/proto/shadow"
)

// Handler gets every shadow update of one device, one at a time and in order.
type Handler func(obj api.Object, s *shadowpb.Shadow)

// Run reads USERNAME, PASSWORD, API_URL, TOKEN_REFRESH_INTERVAL_SECS and
// DEVICE_REFRESH_INTERVAL_SECS, opens one shadow stream per namespace the
// account can see, and calls h for each shadow update. It does not return.
func Run(h Handler) {
	tokenRefresh, err := config.TokenRefresh()
	if err != nil {
		log.Fatal(err)
	}
	nsRefresh, err := config.DeviceRefresh()
	if err != nil {
		log.Fatal(err)
	}
	apiUrl := config.Env("API_URL")
	if apiUrl == "" {
		log.Fatal("API_URL is required")
	}
	conn, err := api.Dial(apiUrl)
	if err != nil {
		log.Fatalf("failed to set up connection to %s: %s", apiUrl, err)
	}
	defer conn.Close()
	handler := api.NewHandler(conn, api.NewTokenHandler(conn, config.Env("USERNAME"), config.Env("PASSWORD"), tokenRefresh))
	Serve(context.Background(), handler, h, nsRefresh)
}

// Serve opens one StreamNamespace per namespace and calls h for each state
// update. Devices added to or removed from a namespace are picked up from the
// stream, so no restart is needed. Namespaces are listed again every
// nsRefresh. Serve returns when ctx is done and all streams have stopped.
func Serve(ctx context.Context, a api.Handler, h Handler, nsRefresh time.Duration) {
	type nsStream struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	streams := map[string]nsStream{}
	defer func() {
		for _, s := range streams {
			s.cancel()
			<-s.done
		}
	}()
	refresh := func() {
		res, err := a.GetNamespaces()
		if err != nil {
			log.Printf("failed to list namespaces: %s\n", err)
			return
		}
		seen := map[string]bool{}
		for _, ns := range res.Namespaces {
			seen[ns.ID] = true
			if _, ok := streams[ns.ID]; ok {
				continue
			}
			sctx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			streams[ns.ID] = nsStream{cancel: cancel, done: done}
			go func(ns api.Namespace) {
				defer close(done)
				serveNamespace(sctx, a, ns, h)
			}(ns)
		}
		for id, s := range streams {
			if !seen[id] {
				s.cancel()
				<-s.done
				delete(streams, id)
			}
		}
	}
	refresh()
	t := time.NewTicker(nsRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refresh()
		}
	}
}

func serveNamespace(ctx context.Context, a api.Handler, ns api.Namespace, h Handler) {
	devices := map[string]api.Object{}
	removed := map[string]bool{}
	for s := range a.StreamNamespace(ctx, ns.ID, nil) {
		id := s.GetDevice()
		if id == "" {
			continue
		}
		obj, ok := devices[id]
		if !ok {
			obj = api.Object{UID: id, Kind: "device", Namespace: ns.ID, NamespaceName: ns.Name}
		}
		if s.GetTitle() != "" {
			obj.Name = s.GetTitle()
		}
		if s.GetTags() != nil {
			obj.Tags = s.GetTags()
		}
		switch s.GetEvent() {
		case shadowpb.ShadowEvent_DEVICE_REMOVED:
			delete(devices, id)
			removed[id] = true
			log.Printf("device %s removed from namespace %s\n", id, ns.ID)
			continue
		case shadowpb.ShadowEvent_DEVICE_ADDED:
			delete(removed, id)
			devices[id] = obj
			log.Printf("device %s added to namespace %s\n", id, ns.ID)
			continue
		case shadowpb.ShadowEvent_STATE:
		default:
			if !removed[id] {
				devices[id] = obj
			}
			continue
		}
		if removed[id] {
			continue
		}
		devices[id] = obj
		h(obj, s)
	}
}
