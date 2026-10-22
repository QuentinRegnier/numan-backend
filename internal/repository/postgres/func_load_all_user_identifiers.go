package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type userIdentifiers struct {
	Username *string
	Email    *string
	Phone    *string
}

func FuncLoadAllUserIdentifiers(ctx context.Context) ([]userIdentifiers, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT username, email, phone FROM auth.func_load_all_user_identifiers()`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les identifiants utilisateurs.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	results := make([]userIdentifiers, 0)
	for rows.Next() {
		var idents userIdentifiers
		if err := rows.Scan(&idents.Username, &idents.Email, &idents.Phone); err == nil {
			results = append(results, idents)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les identifiants utilisateurs.",
			err,
		)
	}

	return results, nil
}
