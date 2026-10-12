package main

import (
	"context"
	"crypto/tls"
	"log"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/batch"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

func main() {
	s, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	opts := influxdb2.DefaultOptions().SetHTTPRequestTimeout(uint(s.Timeout.Seconds()))
	if s.TLSInsecure {
		opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
	}
	client := influxdb2.NewClientWithOptions(s.URL, s.Token, opts)
	defer client.Close()
	w := client.WriteAPIBlocking(s.Org, s.Bucket)
	log.Printf("writing to %s, bucket %s\n", s.URL, s.Bucket)

	b := &batch.Batcher[*write.Point]{Size: s.BatchSize, Interval: s.Flush, MaxBuffer: s.MaxBuffer, Flush: func(points []*write.Point) error {
		if err := w.WritePoint(context.Background(), points...); err != nil {
			if rejected(err) {
				log.Printf("InfluxDB rejected %d points, dropping them: %s\n", len(points), err)
				return nil
			}
			return err
		}
		log.Printf("wrote %d points\n", len(points))
		return nil
	}}
	go b.Run()

	runner.Run(func(obj api.Object, sh *shadowpb.Shadow) {
		m, ok := message.New(obj, sh, false, time.Now())
		if !ok {
			return
		}
		if p, ok := point(s, m); ok {
			b.Add(p)
		}
	})
}
