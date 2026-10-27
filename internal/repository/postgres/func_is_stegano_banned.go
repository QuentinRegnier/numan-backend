package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncIsSteganoBanned vérifie en L3 si l'identifiant stéganographique d'un utilisateur est banni.
func FuncIsSteganoBanned(ctx context.Context, userID int64) (bool, error) {
	if postgres.PostgresDB == nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}
	const query = `SELECT moderation.func_is_stegano_banned($1)`
	var banned bool
	if err := postgres.PostgresDB.QueryRowContext(ctx, query, userID).Scan(&banned); err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier le statut stéganographique.",
			err,
		)
	}
	return banned, nil
}
