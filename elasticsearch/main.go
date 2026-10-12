package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/stream/consumer"
)

const (
	envEsAddr = "ES_ADDR"
	indexName = "infinimesh"
)

// document is what gets indexed for each reported state.
type document struct {
	UID       string                 `json:"uid"`
	Name      string                 `json:"name"`
	Kind      string                 `json:"kind"`
	Version   string                 `json:"version"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// buildDocument returns the document id and body. The id is the device id and
// the state time in nanoseconds, so two updates in the same second differ.
func buildDocument(obj api.Object, s api.DeviceState) (string, []byte, error) {
	t, err := time.Parse(time.RFC3339Nano, s.Timestamp)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse device timestamp: %w", err)
	}
	b, err := json.Marshal(document{UID: obj.UID, Name: obj.Name, Kind: obj.Kind, Version: s.Version, Timestamp: s.Timestamp, Data: s.Data})
	if err != nil {
		return "", nil, err
	}
	return obj.UID + "-" + strconv.FormatInt(t.UnixNano(), 10), b, nil
}

func main() {
	es, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{os.Getenv(envEsAddr)},
	})
	if err != nil {
		log.Fatalf("failed to initialise es client: %v\n", err)
	}
	res, err := es.Ping()
	if err != nil {
		log.Fatalf("failed to ping es: %v\n", err)
	}
	log.Printf("successfully connected to es with result: %v\n", res)
	redisPool, err := config.RedisPool()
	if err != nil {
		log.Fatal(err)
	}
	consumer.New(redisPool).Run(func(event *consumer.DeviceEvent) error {
		id, body, err := buildDocument(event.Object, event.State)
		if err != nil {
			// a bad timestamp will not get better on retry, so drop it
			log.Printf("skipping event: %v\n", err)
			return nil
		}
		res, err := es.Index(indexName, bytes.NewReader(body), es.Index.WithDocumentID(id))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.IsError() {
			return fmt.Errorf("index failed: %s", res.String())
		}
		log.Printf("indexed document %s\n", id)
		return nil
	})
}
