package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncBulkSoftDeleteArticles applique une suppression logique (visibility = -1) en masse sur PostgreSQL L3.
func FuncBulkSoftDeleteArticles(ctx context.Context, articleIDs []int64) error {
	if len(articleIDs) == 0 {
		return nil
	}

	if postgres.WriteDB() == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT content.func_bulk_soft_delete_articles($1)`
	if _, err := postgres.WriteDB().ExecContext(ctx, query, pq.Array(articleIDs)); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de supprimer logiquement les articles en base de données.",
			err,
		)
	}

	return nil
}
