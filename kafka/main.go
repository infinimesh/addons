package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/wrappers"
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

	wrappers.NewObjectManager(
		apiHandler,
		(&workerFactory{api: apiHandler, kafka: kc, cfg: cfg}).NewWorker,
		cfg.DeviceRefresh,
	).Start()
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

type workerFactory struct {
	api   api.Handler
	kafka *kgo.Client
	cfg   *config
}

func (f *workerFactory) NewWorker(obj api.Object) wrappers.Process {
	return &worker{obj: obj, f: f, done: make(chan struct{})}
}

type worker struct {
	obj  api.Object
	f    *workerFactory
	done chan struct{}
}

func (w *worker) Start() {
	// The stream stops when Start returns, which Stop causes.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := w.f.api.GetDeviceShadowStream(ctx, w.obj.UID)
	if err != nil {
		log.Printf("error on get device shadow stream: %s\n", err)
		return
	}
	for {
		select {
		case <-w.done:
			return
		case s := <-ch:
			key, value, ok := message.Build(w.obj, s, w.f.cfg.OnlyReported, time.Now())
			if !ok {
				continue
			}
			w.f.kafka.Produce(context.Background(), &kgo.Record{Key: key, Value: value}, func(r *kgo.Record, err error) {
				if err != nil {
					log.Printf("failed to produce for device %s: %s\n", w.obj.UID, err)
					return
				}
				log.Printf("produced device %s to %s partition %d offset %d\n", w.obj.UID, r.Topic, r.Partition, r.Offset)
			})
		}
	}
}

func (w *worker) Stop() {
	close(w.done)
}
