package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
)

func TestWatermarkProbes_HealthzAndReadyz(t *testing.T) {
	prevSecret := secretKey
	prevClient := minioClient
	prevShutdown := shuttingDown.Load()
	t.Cleanup(func() {
		secretKey = prevSecret
		minioClient = prevClient
		shuttingDown.Store(prevShutdown)
	})

	mux := newWatermarkMux()

	// /healthz répond toujours 200
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"alive"}`, w.Body.String())

	// /readyz répond 503 si non initialisé
	secretKey = ""
	minioClient = nil
	shuttingDown.Store(false)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// /readyz répond 200 quand prêt
	secretKey = "test-secret"
	minioClient = &minio.Client{}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ready"}`, w.Body.String())

	// /readyz répond 503 dès que SIGTERM démarre le shutdown
	shuttingDown.Store(true)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.JSONEq(t, `{"status":"shutting_down"}`, w.Body.String())
}

func TestNewWatermarkServer_ExplicitTimeouts(t *testing.T) {
	srv := newWatermarkServer("8080", newWatermarkMux())
	assert.Equal(t, ":8080", srv.Addr)
	assert.Equal(t, 5*1e9, float64(srv.ReadHeaderTimeout))
	assert.Equal(t, 30*1e9, float64(srv.ReadTimeout))
	assert.Equal(t, 60*1e9, float64(srv.WriteTimeout))
	assert.Equal(t, 120*1e9, float64(srv.IdleTimeout))
}
