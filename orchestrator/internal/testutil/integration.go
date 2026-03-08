package testutil

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func EnvOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func RequireTCP(t *testing.T, address string) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", address, 750*time.Millisecond)
	if err != nil {
		t.Skipf("skipping integration test; service %s unavailable: %v", address, err)
		return
	}
	_ = conn.Close()
}

func ExtractHostPort(rawAddr string, defaultPort string) (string, error) {
	addr := strings.TrimSpace(rawAddr)
	if addr == "" {
		return "", fmt.Errorf("empty address")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	parsed, err := url.Parse(addr)
	if err != nil {
		return "", err
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("invalid address: %q", rawAddr)
	}
	port := parsed.Port()
	if port == "" {
		port = defaultPort
	}
	return net.JoinHostPort(host, port), nil
}

func UniqueID(prefix string) string {
	now := time.Now().UTC().UnixNano()
	if prefix == "" {
		prefix = "test"
	}
	return fmt.Sprintf("%s-%d", prefix, now)
}
