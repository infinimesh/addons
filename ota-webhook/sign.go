package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

const signatureHeader = "X-Infinimesh-Signature"

func sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func signed(secret, header string, body []byte) bool {
	if secret == "" || header == "" {
		return false
	}
	return hmac.Equal([]byte(header), []byte(sign(secret, body)))
}
