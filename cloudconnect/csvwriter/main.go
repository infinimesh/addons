package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/wrappers"
)

const (
	envApiUrl   = "API_URL"
	envUsername = "USERNAME"
	envPassword = "PASSWORD"
	envWriteDir = "WRITE_DIR"
)

func main() {
	username := os.Getenv(envUsername)
	password := os.Getenv(envPassword)
	apiUrl := os.Getenv(envApiUrl)
	tokenRefresh, err := config.TokenRefresh()
	if err != nil {
		log.Fatal(err)
	}
	deviceRefresh, err := config.DeviceRefresh()
	if err != nil {
		log.Fatal(err)
	}
	conn, err := api.Dial(apiUrl)
	if err != nil {
		log.Fatalf("failed to set up connection to %s: %s", apiUrl, err)
	}
	defer conn.Close()
	apiHandler := api.NewHandler(conn, api.NewTokenHandler(conn, username, password, tokenRefresh))
	writeDir := os.Getenv(envWriteDir)
	wrappers.NewObjectManager(
		apiHandler,
		(&objectWorkerFactory{
			api:      apiHandler,
			writeDir: writeDir,
		}).NewObjectWorker,
		deviceRefresh,
	).Start()
}

type objectWorkerFactory struct {
	api      api.Handler
	writeDir string
}

func (f *objectWorkerFactory) NewObjectWorker(obj api.Object) wrappers.Process {
	return &objectWorker{
		obj:      obj,
		api:      f.api,
		writeDir: filepath.Join(f.writeDir, obj.UID),
		done:     make(chan struct{}),
	}
}

type objectWorker struct {
	obj      api.Object
	api      api.Handler
	writeDir string
	done     chan struct{}
}

func (w *objectWorker) Start() {
	// The stream stops when Start returns, which Stop causes.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := w.api.GetDevicesStateStream(ctx, w.obj.UID)
	if err != nil {
		log.Printf("error on get devices state stream: %s\n", err)
		return
	}

	err = os.MkdirAll(w.writeDir, os.ModePerm)
	if err != nil {
		log.Fatalf("failed to mkdir: %v", err) // this should not happen
	}
	writer := &writer{
		writeDir: w.writeDir,
	}

	for {
		select {
		case <-w.done:
			return
		case state := <-ch:
			if state == nil {
				log.Printf("received nil state for object %v", w.obj)
				continue
			}
			record := csvRecord(w.obj, state)
			err := writer.Write(record)
			if err != nil {
				log.Printf("failed to write record to csv: %v\n", err)
			} else {
				log.Printf("successfully wrote record to csv: %v\n", record)
			}
		}
	}
}

func (w *objectWorker) Stop() {
	close(w.done)
}

// csvRecord maps a device state to one CSV row: uid, name, kind, timestamp,
// version and the reported data as JSON.
func csvRecord(obj api.Object, state *api.DevicesStateRes) []string {
	j, _ := json.Marshal(state.Result.ReportedState.Data)
	return []string{
		obj.UID,
		obj.Name,
		obj.Kind,
		state.Result.ReportedState.Timestamp,
		state.Result.ReportedState.Version,
		string(j),
	}
}
