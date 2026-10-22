package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncGetRelationState interroge directement la fonction SQL compilée pour obtenir l'état.
func FuncGetRelationState(ctx context.Context, callerID int64, targetID int64) (int, error) {
	if postgres.PostgresDB == nil {
		return 0, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT auth.func_get_relation_state($1, $2)`

	var state int
	err := postgres.PostgresDB.QueryRowContext(ctx, query, callerID, targetID).Scan(&state)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier l'état de la relation.",
			err,
		)
	}

	return state, nil
}
