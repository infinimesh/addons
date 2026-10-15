package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
	"github.com/infinimesh/addons/sdk/api"
	shadowpb "github.com/infinimesh/proto/shadow"
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
	lookup func(context.Context, string) (string, error)
	mu     sync.Mutex
	held   []heldDevice
}

type heldDevice struct {
	ns  string
	msg *shadowpb.Shadow
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
	for _, ns := range a.namespaceIDs() {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			if err := a.runNS(ctx, ns); err != nil {
				errc <- err
				cancel()
			}
		}(ns)
	}
	every := a.poll
	if every <= 0 {
		every = 15 * time.Second
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := a.releaseHeld(ctx, ""); err != nil {
					errc <- err
					cancel()
					return
				}
				if err := a.refreshURLs(ctx); err != nil {
					errc <- err
					cancel()
					return
				}
				if a.poll > 0 {
					a.flush(ctx)
				}
			}
		}
	}()
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

func (a *app) namespaceIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.urls))
	for ns := range a.urls {
		out = append(out, ns)
	}
	return out
}

func (a *app) urlOf(ns string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.urls[ns]
}

func (a *app) setURL(ns, url string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.urls == nil {
		a.urls = map[string]string{}
	}
	a.urls[ns] = url
}

func (a *app) hold(ns string, msg *shadowpb.Shadow) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held = append(a.held, heldDevice{ns: ns, msg: msg})
}

func (a *app) dropHeld(ns string, msg *shadowpb.Shadow) {
	if msg == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.held[:0]
	for _, h := range a.held {
		if h.ns == ns && h.msg.GetDevice() == msg.GetDevice() && h.msg.GetTitle() == msg.GetTitle() && sameTags(h.msg.GetTags(), msg.GetTags()) {
			continue
		}
		out = append(out, h)
	}
	a.held = out
}

func (a *app) pastHeld(ns string, msg *shadowpb.Shadow) bool {
	if msg == nil || msg.GetEventId() == 0 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	saw := false
	for _, h := range a.held {
		if h.ns != ns {
			continue
		}
		saw = true
		if msg.GetEventId() <= h.msg.GetEventId() {
			return false
		}
	}
	return saw
}

func (a *app) releaseHeld(ctx context.Context, ns string) error {
	a.mu.Lock()
	var mine, rest []heldDevice
	for _, h := range a.held {
		if ns == "" || h.ns == ns {
			mine = append(mine, h)
			continue
		}
		rest = append(rest, h)
	}
	a.held = rest
	a.mu.Unlock()
	for _, h := range mine {
		if a.book.sameDevice(h.ns, h.msg) {
			continue
		}
		if err := a.deliver(ctx, h.ns, h.msg); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) refreshURLs(ctx context.Context) error {
	if a.lookup == nil {
		return nil
	}
	for _, ns := range a.namespaceIDs() {
		got, err := a.lookup(ctx, ns)
		if err != nil {
			if strings.Contains(err.Error(), "token is revoked") {
				return api.ErrTokenRevoked
			}
			a.book.once(ns, "could not read the namespace webhook_url")
			continue
		}
		got = strings.TrimSpace(got)
		if got == "" {
			a.book.once(ns, "webhook_url is required when provider is webhook")
			continue
		}
		if got == a.urlOf(ns) {
			continue
		}
		log.Printf("namespace %s webhook url is %s\n", ns, got)
		a.setURL(ns, got)
	}
	return nil
}
