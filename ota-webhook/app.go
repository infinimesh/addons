package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
	"github.com/infinimesh/addons/sdk/api"
)

type app struct {
	secret string
	urls   map[string]string
	listen string
	poll   time.Duration
	open   otacore.StreamOpen
	writer otacore.Writer
	post   *poster
	book   *book
	now    func() time.Time
}

func (a *app) Run(ctx context.Context) error {
	if a.now == nil {
		a.now = time.Now
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := &http.Server{
		Addr:              a.listen,
		Handler:           http.HandlerFunc(a.serveHTTP),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, len(a.urls)+2)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errc <- err
			cancel()
		}
	}()
	go func() {
		<-ctx.Done()
		sh, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = srv.Shutdown(sh)
	}()
	a.flush(ctx)
	var wg sync.WaitGroup
	for ns := range a.urls {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			if err := a.runNS(ctx, ns); err != nil {
				errc <- err
				cancel()
			}
		}(ns)
	}
	if a.poll > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(a.poll)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					a.flush(ctx)
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(errc)
	}()
	var first error
	for err := range errc {
		if err == nil || first != nil {
			continue
		}
		first = err
		cancel()
	}
	return first
}

func (a *app) runNS(ctx context.Context, ns string) error {
	ch, errc := a.open(ctx, ns, a.book.seqOf(ns))
	for msg := range ch {
		if err := a.handle(ctx, ns, msg); err != nil {
			return err
		}
		a.book.advance(ns, msg.GetSequence())
	}
	if errc == nil {
		return nil
	}
	return <-errc
}

func newApp(cfg config, book *book, open otacore.StreamOpen, writer otacore.Writer) *app {
	p := newPoster(cfg.Secret)
	return &app{
		secret: cfg.Secret,
		urls:   cfg.URLs,
		listen: cfg.Listen,
		poll:   cfg.Poll,
		open:   open,
		writer: writer,
		post:   p,
		book:   book,
		now:    time.Now,
	}
}

func openStreams(h api.Handler, token string) otacore.StreamOpen {
	return otacore.OpenNamespace(h, token)
}
