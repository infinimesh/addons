package api

import (
	"crypto/tls"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Dial opens a gRPC connection to the infinimesh API.
//
// apiUrl is host:port, for example api.example.com:443 or localhost:8000.
// An http:// prefix means plain text (h2c), which is what the api container
// serves on port 8000 inside docker compose. An https:// prefix or no prefix
// means TLS with normal certificate checks.
func Dial(apiUrl string) (*grpc.ClientConn, error) {
	target, plain := parseApiUrl(apiUrl)
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if plain {
		creds = insecure.NewCredentials()
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(creds))
}

func parseApiUrl(apiUrl string) (target string, plain bool) {
	target = strings.TrimSpace(apiUrl)
	switch {
	case strings.HasPrefix(target, "http://"):
		target, plain = strings.TrimPrefix(target, "http://"), true
	case strings.HasPrefix(target, "https://"):
		target = strings.TrimPrefix(target, "https://")
	}
	target = strings.TrimSuffix(target, "/")
	if !strings.Contains(target, ":") {
		if plain {
			target += ":80"
		} else {
			target += ":443"
		}
	}
	return target, plain
}
