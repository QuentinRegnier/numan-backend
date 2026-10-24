package postgres

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// graphSeedPayload est une structure ultra-légère dédiée à l'initialisation de la mémoire
type graphSeedPayload struct {
	ID               int64
	Hashtags         []string
	IndirectHashtags []string // ✅ NOUVEAU
	CreatedAt        time.Time
}

// FuncLoadPostsForGraphSeeding ramène l'historique sémantique complet trié du plus vieux au plus récent
func FuncLoadPostsForGraphSeeding(ctx context.Context) ([]graphSeedPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, hashtags, indirect_hashtags, created_at FROM content.func_load_posts_for_graph_seeding()`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données pour l'amorçage du graphe.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	seeds := make([]graphSeedPayload, 0)
	for rows.Next() {
		var p graphSeedPayload
		if err := rows.Scan(&p.ID, pq.Array(&p.Hashtags), pq.Array(&p.IndirectHashtags), &p.CreatedAt); err == nil { // ✅ NOUVEAU (Scan correct)
			seeds = append(seeds, p)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les données d'amorçage du graphe.",
			err,
		)
	}

	return seeds, nil
}
