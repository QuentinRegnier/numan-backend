package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type relationAddable struct {
	TargetID int64
	State    int
}

// FuncLoadAddableRelations appelle la fonction SQL pure pour récupérer les abonnements et amis.
func FuncLoadAddableRelations(ctx context.Context, callerID int64) ([]relationAddable, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT target_id, state FROM auth.func_load_addable_relations($1)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, callerID)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les relations ajoutables.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	relations := make([]relationAddable, 0)
	for rows.Next() {
		var r relationAddable
		if err := rows.Scan(&r.TargetID, &r.State); err == nil {
			relations = append(relations, r)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les relations ajoutables.",
			err,
		)
	}

	return relations, nil
}
