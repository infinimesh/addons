package main

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
)

func (a *app) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if !signed(a.secret, r.Header.Get(signatureHeader), body) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	in, err := parseInbound(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := a.now().UTC().Format(time.RFC3339)
	stamp(in.Desired, now)
	stamp(in.Reported, now)
	if err := a.writer.Patch(r.Context(), otacore.Patch{Device: in.Device, Desired: in.Desired, Reported: in.Reported}); err != nil {
		code, msg := writeStatus(err)
		http.Error(w, msg, code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeStatus(err error) (int, string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "ota field"),
		strings.Contains(msg, "ota sha256"),
		strings.Contains(msg, "ota size"),
		strings.Contains(msg, "ota status"),
		strings.Contains(msg, "ota updated_at"),
		strings.Contains(msg, "must be a string"),
		strings.Contains(msg, "addon token can write desired ota only"):
		return http.StatusBadRequest, msg
	default:
		return http.StatusBadGateway, msg
	}
}
