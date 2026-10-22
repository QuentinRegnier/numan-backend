package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncGetPasskeyByCredentialID charge une Passkey par son CredentialID depuis PostgreSQL (L3).
func FuncGetPasskeyByCredentialID(ctx context.Context, credentialID string) (two_factor_models.PasskeyPayload, error) {
	if postgres.PostgresDB == nil {
		return two_factor_models.PasskeyPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	const query = `SELECT id, user_id, credential_id, public_key, attestation_type, aaguid, sign_count, device_name, transports, created_at, updated_at FROM auth.func_get_passkey_by_credential_id($1)`

	var p two_factor_models.PasskeyPayload
	var aaguid []byte
	var transports pq.StringArray

	err := postgres.PostgresDB.QueryRowContext(ctx, query, credentialID).Scan(
		&p.ID,
		&p.UserID,
		&p.CredentialID,
		&p.PublicKey,
		&p.AttestationType,
		&aaguid,
		&p.SignCount,
		&p.DeviceName,
		&transports,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return two_factor_models.PasskeyPayload{}, nil
		}
		return two_factor_models.PasskeyPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer la clé d'accès (Passkey).",
			err,
		)
	}

	p.AAGUID = aaguid
	p.Transports = []string(transports)
	return p, nil
}
