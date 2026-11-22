package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Log est l'instance globale thread-safe que toute l'application utilisera.
var Log zerolog.Logger

// On garde une référence globale vers le writer asynchrone pour pouvoir le fermer proprement.
var (
	loggerMu       sync.Mutex
	asyncWriter    diode.Writer
	hasAsyncWriter bool
)

// shouldLogToFile détermine si la rotation de logs sur disque (Lumberjack) doit être activée.
// En production/Kubernetes (readOnlyRootFilesystem: true, GIN_MODE=release ou LOG_TO_FILE=false),
// on loggue uniquement sur stdout (principe 12-Factor) afin de ne jamais tenter d'écrire sur disque.
func shouldLogToFile() bool {
	if raw := strings.TrimSpace(os.Getenv("LOG_TO_FILE")); raw != "" {
		return strings.EqualFold(raw, "true") || raw == "1"
	}
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release")
}

func InitLogger(c context.Context) {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	var output io.Writer = os.Stdout
	hasAsyncWriter = false

	if shouldLogToFile() {
		logPath := strings.TrimSpace(os.Getenv("LOG_FILE_PATH"))
		if logPath == "" {
			logPath = "logs/numan.log"
		}

		// 1. ROTATION DES LOGS (Lumberjack — mode dev / opt-in uniquement)
		fileLogger := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    50,
			MaxBackups: 5,
			MaxAge:     28,
			Compress:   true,
		}

		// 2. ÉCRITURE ASYNCHRONE / BUFFERISÉE (Diode)
		asyncWriter = diode.NewWriter(fileLogger, 10000, 10*time.Millisecond, func(missed int) {
			fmt.Printf("⚠️ LOGGER WARNING: Buffer plein, %d messages abandonnés\n", missed)
		})
		hasAsyncWriter = true
		output = zerolog.MultiLevelWriter(os.Stdout, asyncWriter)
	}

	// 3. ÉCHANTILLONNAGE ANTI-DDOS (BurstSampler)
	sampler := &zerolog.BurstSampler{
		Burst:       50,
		Period:      1 * time.Second,
		NextSampler: &zerolog.BasicSampler{N: 100},
	}

	// 4. CONFIGURATION DU FORMAT JSON NOUVELLE GÉNÉRATION
	zerolog.TimeFieldFormat = time.RFC3339Nano

	// Initialisation de l'instance globale
	Log = zerolog.New(output).
		Sample(sampler).
		With().
		Timestamp().
		Caller().
		Logger()
}

// CloseLogger vide proprement le buffer asynchrone sur le disque et ferme la goroutine
// uniquement lorsque la sortie fichier a été activée.
func CloseLogger(c context.Context) {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	if !hasAsyncWriter {
		return
	}
	hasAsyncWriter = false

	if err := asyncWriter.Close(); err != nil {
		fmt.Printf("Erreur lors de la fermeture du writer asynchrone: %v\n", err)
	}
}
