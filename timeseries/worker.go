package main

import (
	"context"
	"log"

	redistimeseries "github.com/RedisTimeSeries/redistimeseries-go"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/wrappers"
)

type objectWorkerFactory struct {
	api         api.Handler
	redisClient *redistimeseries.Client
}

func (f *objectWorkerFactory) NewObjectWorker(obj api.Object) wrappers.Process {
	return &objectWorker{
		obj:         obj,
		api:         f.api,
		redisClient: f.redisClient,
		done:        make(chan struct{}),
	}
}

type objectWorker struct {
	obj         api.Object
	api         api.Handler
	redisClient *redistimeseries.Client
	done        chan struct{}
}

func (w *objectWorker) Start() {
	createOpts := redistimeseries.CreateOptions{Labels: seriesLabels(w.obj)}

	// The stream stops when Start returns, which Stop causes.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := w.api.GetDevicesStateStream(ctx, w.obj.UID)
	if err != nil {
		log.Printf("error on get devices state stream: %s\n", err)
	}

	createdKeys := map[string]bool{}

	for {
		select {
		case <-w.done:
			return
		case state := <-ch:
			if state == nil {
				log.Printf("received nil state for object %v", w.obj)
				continue
			}
			for _, p := range seriesPoints(w.obj, state) {
				k := p.Key
				f := p.Value
				if !createdKeys[k] {
					_ = w.redisClient.CreateKeyWithOptions(k, createOpts)
					createdKeys[k] = true
				}
				_, err = w.redisClient.AddAutoTsWithOptions(k, f, createOpts)
				if err != nil {
					log.Printf("failed to add time series item: %s\n", err)
					continue
				}
				log.Printf("added time series item: object=%v key=%s\n", w.obj, k)
			}
		}
	}
}

func (w *objectWorker) Stop() {
	close(w.done)
}

// point is one sample: the series key "<data key>:<device uid>" and its value.
type point struct {
	Key   string
	Value float64
}

// seriesLabels are the labels set on every series of a device.
func seriesLabels(obj api.Object) map[string]string {
	return map[string]string{"uid": obj.UID, "name": obj.Name, "kind": obj.Kind}
}

// seriesPoints maps the reported data of a device to samples. Only numbers are
// kept; nil values and other types are skipped.
func seriesPoints(obj api.Object, state *api.DevicesStateRes) []point {
	var ret []point
	for k, v := range state.Result.ReportedState.Data {
		f, ok := v.(float64)
		if !ok {
			if v != nil {
				log.Printf("invalid data type found for object %v and key %s", obj, k)
			}
			continue
		}
		ret = append(ret, point{Key: k + ":" + obj.UID, Value: f})
	}
	return ret
}
