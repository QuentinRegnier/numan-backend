package numan_log

import (
	"errors"
	"time"

	"github.com/rs/zerolog"
)

// LogBuilder enveloppe l'événement Zerolog pour fournir une API fluide strictement typée DDD.
type LogBuilder struct {
	event *zerolog.Event
}

// Err analyse l'erreur fournie. S'il s'agit d'une AppError[cite: 1], le builder extrait
// automatiquement le code métier, le statut HTTP et l'erreur bas-niveau.
func (b *LogBuilder) Err(err error) *LogBuilder {
	if err == nil {
		return b
	}
	var codeErr interface {
		GetCode() string
		GetHTTPStatus() int
		Unwrap() error
	}
	if errors.As(err, &codeErr) {
		b.event.Err(codeErr.Unwrap()).
			Str(LogKeyErrorCode, codeErr.GetCode()).
			Int(LogKeyHTTPStatus, codeErr.GetHTTPStatus())
	} else {

		b.event.Err(err)
	}
	return b
}

// Entity standardise la journalisation d'une entité manipulée (ex: "post", "user") et de son ID.
func (b *LogBuilder) Entity(entityType string, id int64) *LogBuilder {
	b.event.Str(LogKeyEntityType, entityType).Int64(LogKeyEntityID, id)
	return b
}

// Action standardise l'action effectuée sur l'entité (ex: "CREATE", "UPDATE", "SOFT_DELETE").
func (b *LogBuilder) Action(actionType string) *LogBuilder {
	b.event.Str(LogKeyAction, actionType)
	return b
}

// Duration enregistre une métrique de performance (latence, temps de calcul).
func (b *LogBuilder) Duration(key string, d time.Duration) *LogBuilder {
	b.event.Dur(key, d)
	return b
}

// --- Wrappers Standards ---

func (b *LogBuilder) Str(key, val string) *LogBuilder {
	b.event.Str(key, val)
	return b
}

func (b *LogBuilder) Int(key string, val int) *LogBuilder {
	b.event.Int(key, val)
	return b
}

func (b *LogBuilder) Int64(key string, val int64) *LogBuilder {
	b.event.Int64(key, val)
	return b
}

func (b *LogBuilder) Float64(key string, val float64) *LogBuilder {
	b.event.Float64(key, val)
	return b
}

func (b *LogBuilder) float64(key string, val float64) *LogBuilder {
	b.event.Float64(key, val)
	return b
}

func (b *LogBuilder) Bool(key string, val bool) *LogBuilder {
	b.event.Bool(key, val)
	return b
}

func (b *LogBuilder) Interface(key string, val any) *LogBuilder {
	b.event.Interface(key, val)
	return b
}

// --- Terminaisons (Émission du log) ---

// Msg finalise et écrit le log avec un message descriptif.
func (b *LogBuilder) Msg(msg string) {
	b.event.Msg(msg)
}

// Msgf finalise et écrit le log avec un message formaté.
func (b *LogBuilder) Msgf(format string, args ...any) {
	b.event.Msgf(format, args...)
}

func (b *LogBuilder) Uint32(key string, val uint32) *LogBuilder {
	b.event.Uint32(key, val)
	return b
}
func (b *LogBuilder) Bytes(key string, val []byte) *LogBuilder {
	b.event.Bytes(key, val)
	return b
}
