package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
)

const (
	envWriteDir = "WRITE_DIR"
)

func main() {
	apiHandler, nsRefresh, closeConn := runner.Connect()
	defer closeConn()
	runner.Serve(context.Background(), apiHandler, newSink(os.Getenv(envWriteDir)).handle, nsRefresh, shadowpb.StateKey_REPORTED)
}

// sink writes reported state updates to one CSV spool folder per device.
type sink struct {
	writeDir string
	mu       sync.Mutex
	writers  map[string]*writer
}

func newSink(writeDir string) *sink {
	return &sink{writeDir: writeDir, writers: map[string]*writer{}}
}

func (k *sink) handle(obj api.Object, s *shadowpb.Shadow) {
	state := api.ShadowToState(s)
	if state == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	w, ok := k.writers[obj.UID]
	if !ok {
		dir := filepath.Join(k.writeDir, obj.UID)
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			log.Printf("failed to mkdir %s: %v\n", dir, err)
			return
		}
		w = &writer{writeDir: dir}
		k.writers[obj.UID] = w
	}
	record := csvRecord(obj, state)
	if err := w.Write(record); err != nil {
		log.Printf("failed to write record to csv: %v\n", err)
	} else {
		log.Printf("successfully wrote record to csv: %v\n", record)
	}
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
