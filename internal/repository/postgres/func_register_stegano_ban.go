package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncRegisterSteganoBan inscrit définitivement l'identifiant stéganographique d'un utilisateur banni pour infraction majeure.
func FuncRegisterSteganoBan(ctx context.Context, userID int64, steganoID string, reason string, sourcePostID int64) error {
	if postgres.PostgresDB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}
	var postArg any
	if sourcePostID > 0 {
		postArg = sourcePostID
	}
	const query = `SELECT moderation.func_register_stegano_ban($1, $2, $3, $4)`
	if _, err := postgres.PostgresDB.ExecContext(ctx, query, userID, steganoID, reason, postArg); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'enregistrer le bannissement stéganographique.",
			err,
		)
	}
	return nil
}
