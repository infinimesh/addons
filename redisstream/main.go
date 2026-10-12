package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/gomodule/redigo/redis"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/wrappers"
)

const (
	envApiUrl       = "API_URL"
	envUsername     = "USERNAME"
	envPassword     = "PASSWORD"
	envStreamMaxLen = "REDIS_STREAM_MAXLEN"
)

func main() {
	redisPool, err := config.RedisPool()
	if err != nil {
		log.Fatal(err)
	}
	maxLen, err := config.Int(envStreamMaxLen, 100000)
	if err != nil {
		log.Fatal(err)
	}
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

	wrappers.NewObjectManager(
		apiHandler,
		(&objectWorkerFactory{
			api:    apiHandler,
			redis:  redisPool,
			maxLen: maxLen,
		}).NewObjectWorker,
		deviceRefresh,
	).Start()
}

type objectWorkerFactory struct {
	api    api.Handler
	redis  *redis.Pool
	maxLen int
}

func (f *objectWorkerFactory) NewObjectWorker(obj api.Object) wrappers.Process {
	return &objectWorker{
		obj:    obj,
		api:    f.api,
		redis:  f.redis,
		maxLen: f.maxLen,
		done:   make(chan struct{}),
	}
}

type objectWorker struct {
	obj    api.Object
	api    api.Handler
	redis  *redis.Pool
	maxLen int
	done   chan struct{}
}

func (w *objectWorker) Start() {
	ch, err := w.api.GetDevicesStateStream(w.obj.UID)
	if err != nil {
		log.Printf("error on get devices state stream: %s\n", err)
		return
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
			objJson, _ := json.Marshal(w.obj)
			stateJson, _ := json.Marshal(state.Result.ReportedState)
			conn := w.redis.Get()
			reply, err := conn.Do("XADD", xaddArgs(w.maxLen, string(objJson), string(stateJson))...)
			conn.Close()
			if err != nil {
				log.Printf("failed to stream object: object=%v err=%v\n", w.obj, err)
			} else {
				id, _ := redis.String(reply, nil)
				log.Printf("successfully streamed object: object=%v id=%s\n", w.obj, id)
			}
		}
	}
}

func (w *objectWorker) Stop() {
	close(w.done)
}

// xaddArgs builds the XADD arguments. With maxLen above 0 the stream is
// trimmed to about that many entries.
func xaddArgs(maxLen int, obj, state string) []interface{} {
	args := []interface{}{"objects"}
	if maxLen > 0 {
		args = append(args, "MAXLEN", "~", maxLen)
	}
	return append(args, "*", "object", obj, "state", state)
}
