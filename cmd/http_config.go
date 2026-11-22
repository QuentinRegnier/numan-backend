package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPReadHeaderTimeout = 5 * time.Second
	defaultHTTPReadTimeout       = 60 * time.Second
	defaultHTTPWriteTimeout      = 60 * time.Second
	defaultHTTPIdleTimeout       = 120 * time.Second
)

// HTTPTimeouts définit des délais sûrs contre Slowloris (ReadHeaderTimeout court)
// sans couper les uploads mobiles 4G/5G ni le streaming de médias.
type HTTPTimeouts struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func parseTimeoutSecondsEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		return fallback
	}
	return time.Duration(sec) * time.Second
}

// ResolveHTTPTimeouts résout les timeouts du serveur HTTP principal depuis l'environnement.
func ResolveHTTPTimeouts() HTTPTimeouts {
	return HTTPTimeouts{
		ReadHeaderTimeout: parseTimeoutSecondsEnv("HTTP_READ_HEADER_TIMEOUT_SEC", defaultHTTPReadHeaderTimeout),
		ReadTimeout:       parseTimeoutSecondsEnv("HTTP_READ_TIMEOUT_SEC", defaultHTTPReadTimeout),
		WriteTimeout:      parseTimeoutSecondsEnv("HTTP_WRITE_TIMEOUT_SEC", defaultHTTPWriteTimeout),
		IdleTimeout:       parseTimeoutSecondsEnv("HTTP_IDLE_TIMEOUT_SEC", defaultHTTPIdleTimeout),
	}
}
