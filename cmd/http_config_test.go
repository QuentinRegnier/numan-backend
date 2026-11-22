package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResolveHTTPTimeouts(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("HTTP_READ_HEADER_TIMEOUT_SEC", "")
		t.Setenv("HTTP_READ_TIMEOUT_SEC", "")
		t.Setenv("HTTP_WRITE_TIMEOUT_SEC", "")
		t.Setenv("HTTP_IDLE_TIMEOUT_SEC", "")

		got := ResolveHTTPTimeouts()
		assert.Equal(t, 5*time.Second, got.ReadHeaderTimeout)
		assert.Equal(t, 60*time.Second, got.ReadTimeout)
		assert.Equal(t, 60*time.Second, got.WriteTimeout)
		assert.Equal(t, 120*time.Second, got.IdleTimeout)
	})

	t.Run("env overrides and invalid fallback", func(t *testing.T) {
		t.Setenv("HTTP_READ_HEADER_TIMEOUT_SEC", "8")
		t.Setenv("HTTP_READ_TIMEOUT_SEC", "90")
		t.Setenv("HTTP_WRITE_TIMEOUT_SEC", "0")
		t.Setenv("HTTP_IDLE_TIMEOUT_SEC", "invalid")

		got := ResolveHTTPTimeouts()
		assert.Equal(t, 8*time.Second, got.ReadHeaderTimeout)
		assert.Equal(t, 90*time.Second, got.ReadTimeout)
		assert.Equal(t, 60*time.Second, got.WriteTimeout)
		assert.Equal(t, 120*time.Second, got.IdleTimeout)
	})
}
