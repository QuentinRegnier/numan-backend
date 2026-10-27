package test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// FakeMinioServer simule un stockage objet compatible S3 en mémoire RAM.
type FakeMinioServer struct {
	Server  *httptest.Server
	mu      sync.RWMutex
	Buckets map[string]map[string][]byte
}

// SetupTestMinio lance le serveur HTTP local et retourne un client minio configuré.
func SetupTestMinio(t *testing.T) (*FakeMinioServer, *minio.Client) {
	t.Helper()

	fakeS3 := &FakeMinioServer{
		Buckets: make(map[string]map[string][]byte),
	}

	// Lancement du serveur HTTP en mémoire
	fakeS3.Server = httptest.NewServer(http.HandlerFunc(fakeS3.handleRequest))
	t.Cleanup(func() {
		fakeS3.Server.Close()
	})

	// Extraction du port généré aléatoirement (ex: 127.0.0.1:54321)
	endpoint := strings.TrimPrefix(fakeS3.Server.URL, "http://")

	// Injection du client MinIO pointant vers le Fake local
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4("TEST_ACCESS_KEY", "TEST_SECRET_KEY", ""),
		Secure: false, // Obligatoire car httptest.Server n'a pas de certificat valide par défaut
	})
	if err != nil {
		t.Fatalf("Impossible d'initialiser le client MinIO de test: %v", err)
	}

	return fakeS3, client
}

// SeedObject permet d'injecter des données (ex: une image AVIF) directement en RAM avant le test.
func (f *FakeMinioServer) SeedObject(bucket, key string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Buckets[bucket] == nil {
		f.Buckets[bucket] = make(map[string][]byte)
	}
	f.Buckets[bucket][key] = data
}

// handleRequest intercepte les appels S3 (REST) et répond depuis la RAM.
func (f *FakeMinioServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Format S3 basique : /{bucket}/{key}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "Bad Request: Missing bucket", http.StatusBadRequest)
		return
	}
	bucket := parts[0]

	// Simulation du HEAD (BucketExists / ObjectExists)
	if r.Method == http.MethodHead {
		w.Header().Set("ETag", "\"d41d8cd98f00b204e9800998ecf8427e\"")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Requête sur le bucket (ex: GetBucketLocation ou BucketExists)
	if r.Method == http.MethodGet && (len(parts) == 1 || parts[1] == "") {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`)
		return
	}

	// Simulation du GET Object (ex: utilisé par le Watermark Service)
	if r.Method == http.MethodGet && len(parts) == 2 {
		key := parts[1]

		bucketData, ok := f.Buckets[bucket]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code></Error>`)
			return
		}

		data, ok := bucketData[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code></Error>`)
			return
		}

		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, bytes.NewReader(data))
		return
	}

	// Simulation du PUT Object (ex: si le worker sauvegarde le média généré)
	if r.Method == http.MethodPut && len(parts) == 2 {
		f.mu.RUnlock() // Switch to Write Lock
		f.mu.Lock()
		if f.Buckets[bucket] == nil {
			f.Buckets[bucket] = make(map[string][]byte)
		}
		data, _ := io.ReadAll(r.Body)
		f.Buckets[bucket][parts[1]] = data
		f.mu.Unlock()
		f.mu.RLock() // Restore Read Lock for defer

		w.Header().Set("ETag", "\"d41d8cd98f00b204e9800998ecf8427e\"")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method Not Allowed in FakeS3", http.StatusMethodNotAllowed)
}
