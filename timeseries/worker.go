package main

import (
	"log"
	"sync"

	redistimeseries "github.com/RedisTimeSeries/redistimeseries-go"
	"github.com/infinimesh/addons/sdk/api"
	shadowpb "github.com/infinimesh/proto/shadow"
)

// series is the part of the RedisTimeSeries client the sink uses.
type series interface {
	CreateKeyWithOptions(key string, opts redistimeseries.CreateOptions) error
	AddAutoTsWithOptions(key string, value float64, opts redistimeseries.CreateOptions) (int64, error)
}

// sink adds one sample per numeric reported value.
type sink struct {
	client  series
	mu      sync.Mutex
	created map[string]bool
}

func newSink(client series) *sink {
	return &sink{client: client, created: map[string]bool{}}
}

func (k *sink) handle(obj api.Object, s *shadowpb.Shadow) {
	state := api.ShadowToState(s)
	if state == nil {
		return
	}
	opts := redistimeseries.CreateOptions{Labels: seriesLabels(obj)}
	for _, p := range seriesPoints(obj, state) {
		k.mu.Lock()
		if !k.created[p.Key] {
			_ = k.client.CreateKeyWithOptions(p.Key, opts)
			k.created[p.Key] = true
		}
		k.mu.Unlock()
		if _, err := k.client.AddAutoTsWithOptions(p.Key, p.Value, opts); err != nil {
			log.Printf("failed to add time series item: %s\n", err)
			continue
		}
		log.Printf("added time series item: object=%v key=%s\n", obj, p.Key)
	}
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
