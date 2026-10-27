package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncArchiveClosedReport persiste la décision finale et archive le ticket fermé dans la base froide PostgreSQL.
func FuncArchiveClosedReport(ctx context.Context, reportID int64, state int, rationale string) error {
	if postgres.PostgresDB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	const query = `SELECT moderation.func_archive_closed_report($1, $2, $3)`
	var archived bool
	err := postgres.PostgresDB.QueryRowContext(ctx, query, reportID, state, rationale).Scan(&archived)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'archiver le signalement dans la base de données.",
			err,
		)
	}
	return nil
}
