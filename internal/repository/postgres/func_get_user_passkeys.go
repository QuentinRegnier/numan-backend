package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncGetUserPasskeys charge toutes les Passkeys d'un utilisateur depuis PostgreSQL (L3).
func FuncGetUserPasskeys(ctx context.Context, userID int64) ([]two_factor_models.PasskeyPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	const query = `SELECT id, user_id, credential_id, public_key, attestation_type, aaguid, sign_count, device_name, transports, created_at, updated_at FROM auth.func_get_user_passkeys($1)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les clés d'accès (Passkeys).",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	list := make([]two_factor_models.PasskeyPayload, 0)
	for rows.Next() {
		var p two_factor_models.PasskeyPayload
		var aaguid []byte
		var transports pq.StringArray

		if errScan := rows.Scan(
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
		); errScan != nil {
			continue
		}
		p.AAGUID = aaguid
		p.Transports = []string(transports)
		list = append(list, p)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les clés d'accès (Passkeys).",
			err,
		)
	}

	return list, nil
}
