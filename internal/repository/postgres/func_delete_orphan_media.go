package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type orphanMediaResult struct {
	ID          int64
	StoragePath string
}

// FuncDeleteOrphanMedia exécute la purge SQL et retourne les médias détruits.
func FuncDeleteOrphanMedia(ctx context.Context) ([]orphanMediaResult, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, storage_path FROM content.func_delete_orphan_media()`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'exécuter la purge des médias orphelins.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	results := make([]orphanMediaResult, 0)
	for rows.Next() {
		var r orphanMediaResult
		if err := rows.Scan(&r.ID, &r.StoragePath); err == nil {
			results = append(results, r)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les médias orphelins.",
			err,
		)
	}

	return results, nil
}
