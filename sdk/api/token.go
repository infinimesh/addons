package api

import (
	"context"
	"log"
	"sync"
	"time"

	pb "github.com/infinimesh/proto/node"
	accpb "github.com/infinimesh/proto/node/accounts"
	"google.golang.org/grpc"
)

type TokenHandler interface {
	RefreshToken() error
	GetToken() string
}

type tokenHandlerImpl struct {
	mu        sync.RWMutex
	client    pb.AccountsServiceClient
	username  string
	password  string
	lastToken string
}

// NewTokenHandler returns a new TokenHandler that logs in with a standard
// infinimesh account (AccountsService.Token).
//
// Tokens are synchronously retrieved on init, and refreshed automatically per
// tick duration
//
// If a token retrieval attempt fails, GetToken returns the previous token
func NewTokenHandler(conn grpc.ClientConnInterface, username, password string, refreshInterval time.Duration) TokenHandler {
	h := &tokenHandlerImpl{
		client:   pb.NewAccountsServiceClient(conn),
		username: username,
		password: password,
	}
	if err := h.RefreshToken(); err != nil {
		log.Printf("error retrieving token: %s\n", err)
	}
	go func() {
		for range time.Tick(refreshInterval) {
			if err := h.RefreshToken(); err != nil {
				log.Printf("error retrieving token: %s\n", err)
			}
		}
	}()
	return h
}

func (h *tokenHandlerImpl) RefreshToken() error {
	log.Println("refreshing account token...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := h.client.Token(ctx, &pb.TokenRequest{
		Auth: &accpb.Credentials{Type: "standard", Data: []string{h.username, h.password}},
	})
	if err != nil {
		return NewFailedRequestError("AccountsService.Token", err)
	}
	h.mu.Lock()
	h.lastToken = res.GetToken()
	h.mu.Unlock()
	return nil
}

func (h *tokenHandlerImpl) GetToken() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastToken
}
