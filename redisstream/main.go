package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/gomodule/redigo/redis"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
)

const (
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
	apiHandler, nsRefresh, closeConn := runner.Connect()
	defer closeConn()
	runner.Serve(context.Background(), apiHandler, sink(redisPool, maxLen), nsRefresh, shadowpb.StateKey_REPORTED)
}

// sink adds one stream entry per reported state update.
func sink(pool *redis.Pool, maxLen int) runner.Handler {
	return func(obj api.Object, s *shadowpb.Shadow) {
		state := api.ShadowToState(s)
		if state == nil {
			return
		}
		conn := pool.Get()
		reply, err := conn.Do("XADD", entryArgs(maxLen, obj, state)...)
		conn.Close()
		if err != nil {
			log.Printf("failed to stream object: object=%v err=%v\n", obj, err)
			return
		}
		id, _ := redis.String(reply, nil)
		log.Printf("successfully streamed object: object=%v id=%s\n", obj, id)
	}
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

// entryArgs maps a device state to the XADD arguments of one stream entry:
// the object and the reported state, each as JSON.
func entryArgs(maxLen int, obj api.Object, state *api.DevicesStateRes) []interface{} {
	objJson, _ := json.Marshal(obj)
	stateJson, _ := json.Marshal(state.Result.ReportedState)
	return xaddArgs(maxLen, string(objJson), string(stateJson))
}
