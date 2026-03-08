package db

import "testing"

func TestNewClientFailsOnInvalidDSN(t *testing.T) {
	_, err := NewClient("://invalid-dsn")
	if err == nil {
		t.Fatalf("expected error for invalid postgres dsn")
	}
}
