package redis

import (
	"errors"

	goredis "github.com/redis/go-redis/v9"
)

var (
	// ErrNotFound est la sentinelle DDD exposée par le repository pour signaler une absence de clé (cache miss).
	// Elle aliasse goredis.Nil afin de préserver une compatibilité totale avec errors.Is sans fuiter go-redis hors du repository.
	ErrNotFound = goredis.Nil

	// ErrNotInitialized signale qu'une collection ou un client Redis requis n'est pas initialisé.
	ErrNotInitialized = errors.New("collection Redis non initialisée")

	// ErrStopScan permet à un callback de ScanKeysBatched* d'interrompre proprement le parcours SCAN sans erreur.
	ErrStopScan = errors.New("arrêt volontaire du SCAN Redis")
)

// IsNotFound retourne true si l'erreur correspond à une absence de clé Redis (cache miss).
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
