// Package runner holds the common main loop of an addon that streams device
// shadows from the infinimesh API.
package runner

import (
	"log"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/wrappers"
	shadowpb "github.com/infinimesh/proto/shadow"
)

// Handler gets every shadow update of one device, one at a time and in order.
type Handler func(obj api.Object, s *shadowpb.Shadow)

// Run reads USERNAME, PASSWORD, API_URL, TOKEN_REFRESH_INTERVAL_SECS and
// DEVICE_REFRESH_INTERVAL_SECS, starts one worker per device the account can
// see, and calls h for each shadow update. It does not return.
func Run(h Handler) {
	tokenRefresh, err := config.TokenRefresh()
	if err != nil {
		log.Fatal(err)
	}
	deviceRefresh, err := config.DeviceRefresh()
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
	wrappers.NewObjectManager(handler, func(obj api.Object) wrappers.Process {
		return &worker{api: handler, obj: obj, h: h, done: make(chan struct{})}
	}, deviceRefresh).Start()
}

type worker struct {
	api  api.Handler
	obj  api.Object
	h    Handler
	done chan struct{}
}

func (w *worker) Start() {
	ch, err := w.api.GetDeviceShadowStream(w.obj.UID)
	if err != nil {
		log.Printf("error on get device shadow stream for %s: %s\n", w.obj.UID, err)
		return
	}
	for {
		select {
		case <-w.done:
			return
		case s := <-ch:
			if s != nil {
				w.h(w.obj, s)
			}
		}
	}
}

func (w *worker) Stop() { close(w.done) }
