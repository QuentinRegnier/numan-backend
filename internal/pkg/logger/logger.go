package logger

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Log est l'instance globale thread-safe que toute l'application utilisera.
var Log zerolog.Logger

// On garde une référence globale vers le writer asynchrone pour pouvoir le fermer.
var asyncWriter diode.Writer

func InitLogger(c context.Context) {

	// 1. ROTATION DES LOGS (Lumberjack)
	fileLogger := &lumberjack.Logger{
		Filename:   "logs/numan.log",
		MaxSize:    50,
		MaxBackups: 5,
		MaxAge:     28,
		Compress:   true,
	}

	// 2. ÉCRITURE ASYNCHRONE / BUFFERISÉE (Diode)
	asyncWriter = diode.NewWriter(fileLogger, 10000, 10*time.Millisecond, func(missed int) {
		fmt.Printf("⚠️ LOGGER WARNING: Buffer plein, %d messages abandonnés\n", missed)
	})

	multiWriter := zerolog.MultiLevelWriter(os.Stdout, asyncWriter)

	// 3. ÉCHANTILLONNAGE ANTI-DDOS (BurstSampler)
	sampler := &zerolog.BurstSampler{
		Burst:       50,
		Period:      1 * time.Second,
		NextSampler: &zerolog.BasicSampler{N: 100},
	}

	// 4. CONFIGURATION DU FORMAT JSON NOUVELLE GÉNÉRATION
	zerolog.TimeFieldFormat = time.RFC3339Nano

	// Initialisation de l'instance globale
	Log = zerolog.New(multiWriter).
		Sample(sampler).
		With().
		Timestamp().
		Caller().
		Logger()
}

// CloseLogger vide proprement le buffer asynchrone sur le disque et ferme la goroutine.
// À appeler avec un 'defer' dans le main().
func CloseLogger(c context.Context) {
	// diode.Writer étant une struct (valeur), elle est initialisée d'office.
	// On ignore l'erreur de fermeture volontairement pour satisfaire le linter.
	err := asyncWriter.Close()
	if err != nil {
		fmt.Printf("Erreur lors de la fermeture du writer asynchrone", err)
	}
}
