package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	"github.com/gen2brain/avif"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var minioClient *minio.Client
var bucketName string
var secretKey string
var shuttingDown atomic.Bool

const (
	delta                    = 15.0 // Force du tatouage (plus élevé = plus robuste mais plus visible)
	margin                   = 10.0 // Marge de sécurité pour le différentiel DCT
	defaultWatermarkTTLSec   = int64(3600)
	maxWatermarkClockSkewSec = int64(300)
)

func main() {
	log.Println("🚀 Démarrage du micro-service de tatouage (Watermark)...")

	// 1. Chargement des variables d'environnement
	endpoint := os.Getenv("MINIO_ENDPOINT")
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = os.Getenv("S3_ACCESS_KEY")
	}
	minioSecret := os.Getenv("MINIO_SECRET_KEY")
	if minioSecret == "" {
		minioSecret = os.Getenv("S3_SECRET_KEY")
	}
	useSSL := strings.EqualFold(strings.TrimSpace(os.Getenv("USE_SSL")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("S3_USE_SSL")), "true")
	bucketName = os.Getenv("MINIO_BUCKET_NAME")
	secretKey = os.Getenv("WATERMARK_SECRET_KEY")

	if secretKey == "" {
		log.Fatal("❌ ERREUR: WATERMARK_SECRET_KEY n'est pas défini")
	}

	// 2. Initialisation du client S3 (MinIO/Scaleway) isolé
	var err error
	minioClient, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, minioSecret, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalf("❌ ERREUR MinIO: %v", err)
	}

	// 3. Définition du routeur HTTP dédié (avec sondes Kubernetes /healthz et /readyz)
	mux := newWatermarkMux()

	// 4. Lancement du serveur avec Graceful Shutdown (SIGTERM/SIGINT) et timeouts explicites
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	metricsPort := strings.TrimSpace(os.Getenv("METRICS_PORT"))
	if metricsPort == "" {
		metricsPort = "9090"
	}
	srv := newWatermarkServer(port, mux)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsSrv := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()

	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("⚠️ Serveur interne /metrics Watermark arrêté: %v", err)
		}
	}()

	srvErrCh := make(chan error, 1)
	go func() {
		log.Printf("✅ Service Watermark prêt sur le port %s (metrics_port=%s)", port, metricsPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrCh <- err
		}
		close(srvErrCh)
	}()

	select {
	case err := <-srvErrCh:
		if err != nil {
			log.Fatalf("❌ ERREUR Serveur HTTP: %v", err)
		}
	case <-sigCtx.Done():
		shuttingDown.Store(true)
		log.Println("🛑 Signal d'arrêt reçu : vidage des requêtes Watermark en cours...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("⚠️ Arrêt forcé du serveur Watermark: %v", err)
		}
		_ = metricsSrv.Shutdown(shutdownCtx)
		cancel()
		log.Println("✅ Service Watermark arrêté proprement.")
	}
}

func newWatermarkServer(port string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func newWatermarkMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler)
	mux.HandleFunc("/process", processHandler)
	return mux
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"alive"}`))
}

func readyzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if shuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"shutting_down"}`))
		return
	}
	if secretKey == "" || minioClient == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

func resolveWatermarkTTLSeconds() int64 {
	raw := strings.TrimSpace(os.Getenv("WATERMARK_URL_TTL_SECONDS"))
	if raw == "" {
		return defaultWatermarkTTLSec
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return defaultWatermarkTTLSec
	}
	return parsed
}

func validateWatermarkTimestamp(tsRaw string, now time.Time) bool {
	tsVal, err := strconv.ParseInt(strings.TrimSpace(tsRaw), 10, 64)
	if err != nil || tsVal <= 0 {
		return false
	}
	// Normalisation défensive si le timestamp a été transmis en millisecondes
	if tsVal > 1_000_000_000_000 {
		tsVal /= 1000
	}

	nowSec := now.Unix()
	ttlSec := resolveWatermarkTTLSeconds()
	if nowSec-tsVal > ttlSec {
		return false
	}
	if tsVal-nowSec > maxWatermarkClockSkewSec {
		return false
	}
	return true
}

func verifyWatermarkHMAC(payload, clientSig, secret string) bool {
	if secret == "" || clientSig == "" {
		return false
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	expectedSig := hex.EncodeToString(h.Sum(nil))
	normalizedClientSig := strings.ToLower(strings.TrimSpace(clientSig))
	return hmac.Equal([]byte(normalizedClientSig), []byte(expectedSig))
}

func processHandler(w http.ResponseWriter, r *http.Request) {
	// --- A. EXTRACTION DES PARAMÈTRES ---
	q := r.URL.Query()
	key := q.Get("key")
	author := q.Get("author")
	post := q.Get("post")
	if post == "" {
		post = q.Get("post_service")
	}
	reader := q.Get("reader")
	ts := q.Get("ts")
	clientSig := q.Get("sig")

	if key == "" || author == "" || post == "" || reader == "" || ts == "" || clientSig == "" {
		http.Error(w, "Paramètres manquants", http.StatusBadRequest)
		return
	}

	// --- B. VÉRIFICATION DE LA SIGNATURE ET DE L'EXPIRATION (SÉCURITÉ) ---
	// On reconstruit exactement la même chaîne que dans generateWatermarkedURL (helpers.go)
	payload := fmt.Sprintf("key=%s&author=%s&post=%s&reader=%s&ts=%s", key, author, post, reader, ts)
	if !verifyWatermarkHMAC(payload, clientSig, secretKey) {
		log.Printf("⚠️ Signature invalide pour l'image: %s", key)
		http.Error(w, "Accès refusé", http.StatusForbidden)
		return
	}

	if !validateWatermarkTimestamp(ts, time.Now()) {
		log.Printf("⚠️ Lien expiré ou horodatage invalide pour l'image: %s (ts=%s)", key, ts)
		http.Error(w, "Lien expiré", http.StatusForbidden)
		return
	}

	// --- C. TÉLÉCHARGEMENT DE L'IMAGE VIERGE EN RAM ---
	ctx := context.Background()
	object, err := minioClient.GetObject(ctx, bucketName, key, minio.GetObjectOptions{})
	if err != nil {
		http.Error(w, "Image introuvable", http.StatusNotFound)
		return
	}
	defer func(object *minio.Object) {
		err := object.Close()
		if err != nil {
			log.Printf("⚠️ Erreur fermeture stream S3: %v", err)
		}
	}(object)

	// On lit tout le fichier binaire (AVIF) dans la RAM
	imgBytes, err := io.ReadAll(object)
	if err != nil {
		log.Printf("❌ Erreur lecture stream S3: %v", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}

	// Décodage AVIF uniquement
	src, err := avif.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		http.Error(w, "Erreur décodage", http.StatusInternalServerError)
		return
	}

	// Injection du tatouage
	watermarkData := fmt.Sprintf("A:%s|P:%s|R:%s|T:%s", author, post, reader, ts)
	watermarkedImg, err := applyDCTWatermark(src, watermarkData)
	if err != nil {
		http.Error(w, "Erreur traitement", http.StatusInternalServerError)
		return
	}

	// Encodage AVIF final
	var buf bytes.Buffer
	if err := avif.Encode(&buf, watermarkedImg, avif.Options{Quality: 65, Speed: 5}); err != nil {
		http.Error(w, "Erreur encodage", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/avif")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	if _, err := io.Copy(w, &buf); err != nil {
		log.Printf("⚠️ Erreur lors de l'envoi de l'image au client (connexion coupée ?) : %v", err)
	}
}

// applyDCTWatermark implémente l'algorithme Koch-Zhao (Pixels -> DCT -> IDCT)
func applyDCTWatermark(src image.Image, payload string) (image.Image, error) {
	bounds := src.Bounds()
	// On crée une copie modifiable de l'image
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	bits := stringToBits(payload)
	width, height := bounds.Dx(), bounds.Dy()
	bitIdx := 0

	// Parcours par blocs de 8x8
	for y := 0; y < height-8 && bitIdx < len(bits); y += 8 {
		for x := 0; x < width-8 && bitIdx < len(bits); x += 8 {
			injectBitInBlock(dst, x, y, bits[bitIdx])
			bitIdx++
		}
	}

	return dst, nil
}

func clampUint8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

func injectBitInBlock(img *image.RGBA, startX, startY, bit int) {
	var f [8][8]float64
	// 1. Extraction de la luminance
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := img.At(startX+x, startY+y)
			r, g, b, _ := c.RGBA()
			// Y = 0.299R + 0.587G + 0.114B
			f[x][y] = 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
		}
	}

	// 2. Passage dans le domaine fréquentiel (DCT-II)
	F := dct2D(f)

	// 3. Modification des coefficients (Koch-Zhao sur les fréquences moyennes 4,5 et 5,4)
	u1, v1 := 4, 5
	u2, v2 := 5, 4

	if bit == 1 {
		if F[u1][v1] <= F[u2][v2]+margin {
			F[u1][v1] = F[u2][v2] + delta
		}
	} else {
		if F[u2][v2] <= F[u1][v1]+margin {
			F[u2][v2] = F[u1][v1] + delta
		}
	}

	// 4. Retour dans le domaine spatial (IDCT)
	fNew := idct2D(F)

	// 5. Mise à jour des pixels dans l'image en préservant la chrominance RGB d'origine
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			oldColor := img.At(startX+x, startY+y)
			r, g, b, a := oldColor.RGBA()

			diff := fNew[x][y] - f[x][y]
			newR := clampUint8(float64(r>>8) + diff)
			newG := clampUint8(float64(g>>8) + diff)
			newB := clampUint8(float64(b>>8) + diff)

			img.Set(startX+x, startY+y, color.RGBA{
				R: newR,
				G: newG,
				B: newB,
				A: uint8(a >> 8),
			})
		}
	}
}

func dct2D(f [8][8]float64) [8][8]float64 {
	var F [8][8]float64
	for u := 0; u < 8; u++ {
		for v := 0; v < 8; v++ {
			sum := 0.0
			for x := 0; x < 8; x++ {
				for y := 0; y < 8; y++ {
					sum += f[x][y] * math.Cos((float64(2*x+1)*float64(u)*math.Pi)/16.0) * math.Cos((float64(2*y+1)*float64(v)*math.Pi)/16.0)
				}
			}
			alphaU := 1.0
			if u == 0 {
				alphaU = 1.0 / math.Sqrt(2)
			}
			alphaV := 1.0
			if v == 0 {
				alphaV = 1.0 / math.Sqrt(2)
			}
			F[u][v] = 0.25 * alphaU * alphaV * sum
		}
	}
	return F
}

func idct2D(F [8][8]float64) [8][8]float64 {
	var f [8][8]float64
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			sum := 0.0
			for u := 0; u < 8; u++ {
				for v := 0; v < 8; v++ {
					alphaU := 1.0
					if u == 0 {
						alphaU = 1.0 / math.Sqrt(2)
					}
					alphaV := 1.0
					if v == 0 {
						alphaV = 1.0 / math.Sqrt(2)
					}

					sum += alphaU * alphaV * F[u][v] * math.Cos((float64(2*x+1)*float64(u)*math.Pi)/16.0) * math.Cos((float64(2*y+1)*float64(v)*math.Pi)/16.0)
				}
			}
			f[x][y] = 0.25 * sum
		}
	}
	return f
}

func stringToBits(s string) []int {
	var bits []int
	for _, char := range []byte(s) {
		for i := 7; i >= 0; i-- {
			bits = append(bits, int((char>>i)&1))
		}
	}
	return bits
}
