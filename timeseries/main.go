package main

import (
	"log"
	"os"

	redistimeseries "github.com/RedisTimeSeries/redistimeseries-go"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/wrappers"
)

const (
	envUsername = "USERNAME"
	envPassword = "PASSWORD"
	envApiUrl   = "API_URL"
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
	redisPool, err := config.RedisPool()
	if err != nil {
		log.Fatal(err)
	}
	redisClient := redistimeseries.NewClientFromPool(redisPool, "plugin")
	conn, err := api.Dial(apiUrl)
	if err != nil {
		log.Fatalf("failed to set up connection to %s: %s", apiUrl, err)
	}
	defer conn.Close()
	apiHandler := api.NewHandler(conn, api.NewTokenHandler(conn, username, password, tokenRefresh))
	wrappers.NewObjectManager(
		apiHandler,
		(&objectWorkerFactory{
			api:         apiHandler,
			redisClient: redisClient,
		}).NewObjectWorker,
		deviceRefresh,
	).Start()
}
