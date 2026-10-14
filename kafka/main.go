package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	kc, err := kgo.NewClient(cfg.kafkaOpts()...)
	if err != nil {
		log.Fatalf("failed to create Kafka client: %s", err)
	}
	defer kc.Close()
	if err := ensureTopic(kc, cfg); err != nil {
		log.Fatalf("failed to prepare topic %s: %s", cfg.Topic, err)
	}

	conn, err := api.Dial(cfg.ApiUrl)
	if err != nil {
		log.Fatalf("failed to set up connection to %s: %s", cfg.ApiUrl, err)
	}
	defer conn.Close()
	apiHandler := api.NewHandler(conn, api.NewTokenHandler(conn, cfg.Username, cfg.Password, cfg.TokenRefresh))

	runner.Serve(context.Background(), apiHandler, sink(kc, cfg.OnlyReported), cfg.DeviceRefresh)
}

// ensureTopic checks that the cluster answers and creates the topic when
// KAFKA_CREATE_TOPIC is true. A topic that already exists is fine.
func ensureTopic(kc *kgo.Client, cfg *config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := kc.Ping(ctx); err != nil {
		return err
	}
	if !cfg.CreateTopic {
		return nil
	}
	res, err := kadm.NewClient(kc).CreateTopic(ctx, cfg.Partitions, cfg.ReplicationFactor, nil, cfg.Topic)
	if err == nil {
		err = res.Err
	}
	if err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
		return err
	}
	return nil
}

// producer is the part of the Kafka client the sink uses.
type producer interface {
	Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error))
}

// sink produces one record per shadow update, keyed by device.
func sink(kc producer, onlyReported bool) runner.Handler {
	return func(obj api.Object, s *shadowpb.Shadow) {
		key, value, ok := message.Build(obj, s, onlyReported, time.Now())
		if !ok {
			return
		}
		kc.Produce(context.Background(), &kgo.Record{Key: key, Value: value}, func(r *kgo.Record, err error) {
			if err != nil {
				log.Printf("failed to produce for device %s: %s\n", obj.UID, err)
				return
			}
			log.Printf("produced device %s to %s partition %d offset %d\n", obj.UID, r.Topic, r.Partition, r.Offset)
		})
	}
}
