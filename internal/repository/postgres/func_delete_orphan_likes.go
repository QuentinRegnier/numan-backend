package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type orphanLikeTarget struct {
	TargetType int
	TargetID   int64
}

// FuncDeleteOrphanLikes appelle la fonction SQL stockée pour purger les likes orphelins
// et récupère leurs identifiants pour répercuter le nettoyage sur le cache L2 (MongoDB).
func FuncDeleteOrphanLikes(ctx context.Context) ([]orphanLikeTarget, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT target_type, target_id FROM content.func_delete_orphan_likes()`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'exécuter la purge des likes orphelins.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	targets := make([]orphanLikeTarget, 0)
	for rows.Next() {
		var t orphanLikeTarget
		if err := rows.Scan(&t.TargetType, &t.TargetID); err == nil {
			targets = append(targets, t)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les likes orphelins.",
			err,
		)
	}

	return targets, nil
}
