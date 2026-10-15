package main

import (
	"os"
	"strings"
	"testing"
)

func TestComposeLoginAndBind(t *testing.T) {
	b, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, bad := range []string{`"8080:8080"`, "=admin\n", `{noop}admin`} {
		if strings.Contains(s, bad) {
			t.Errorf("docker-compose.yml contains %q", bad)
		}
	}
	for _, want := range []string{
		`"127.0.0.1:8080:8080"`,
		"HAWKBIT_USER=${HAWKBIT_USER:?",
		"HAWKBIT_PASSWORD=${HAWKBIT_PASSWORD:?",
		`"hawkbit.security.user.admin.password":"#{null}"`,
		`"{noop}${HAWKBIT_PASSWORD:?`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("docker-compose.yml lacks %q", want)
		}
	}
}
