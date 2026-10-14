package main

import (
	"context"
	"log"

	redistimeseries "github.com/RedisTimeSeries/redistimeseries-go"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
)

func main() {
	redisPool, err := config.RedisPool()
	if err != nil {
		log.Fatal(err)
	}
	redisClient := redistimeseries.NewClientFromPool(redisPool, "plugin")
	apiHandler, nsRefresh, closeConn := runner.Connect()
	defer closeConn()
	runner.Serve(context.Background(), apiHandler, newSink(redisClient).handle, nsRefresh, shadowpb.StateKey_REPORTED)
}
