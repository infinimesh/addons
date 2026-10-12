package main

import (
	"context"
	"log"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/batch"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	s, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, s.URL)
	if err != nil {
		log.Fatalf("bad POSTGRES_URL: %s", err)
	}
	defer pool.Close()
	for i := 0; ; i++ {
		if err = setup(ctx, pool, s); err == nil {
			break
		}
		if i == 30 {
			log.Fatalf("could not set up table: %s", err)
		}
		log.Printf("waiting for Postgres: %s\n", err)
		time.Sleep(2 * time.Second)
	}
	log.Printf("writing to table %s\n", s.Table.Sanitize())

	b := &batch.Batcher[[]any]{Size: s.BatchSize, Interval: s.Flush, MaxBuffer: s.MaxBuffer, Flush: func(rows [][]any) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := insert(ctx, pool, s.Table, rows); err != nil {
			return err
		}
		log.Printf("inserted %d rows\n", len(rows))
		return nil
	}}
	go b.Run()

	runner.Run(func(obj api.Object, sh *shadowpb.Shadow) {
		if m, ok := message.New(obj, sh, s.OnlyReported, time.Now()); ok {
			b.Add(row(m))
		}
	})
}
