// internal/repository/postgres/func_get_passkeys.go
package postgres

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncGetUserPasskeys charge toutes les Passkeys d'un utilisateur depuis PostgreSQL (L3).
func FuncGetUserPasskeys(ctx context.Context, userID int64) ([]two_factor_models.PasskeyPayload, error) {
	const query = `SELECT id, user_id, credential_id, public_key, attestation_type, aaguid, sign_count, device_name, transports, created_at, updated_at FROM auth.func_get_user_passkeys($1)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("user_id", userID).Msg("Postgres L3 : Échec de func_get_user_passkeys")
		return nil, err
	}
	defer rows.Close()

	var list []two_factor_models.PasskeyPayload
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
			numan_log.Error(ctx).Err(errScan).Msg("Postgres L3 : Échec Scan PasskeyPayload")
			continue
		}
		p.AAGUID = aaguid
		p.Transports = []string(transports)
		list = append(list, p)
	}

	return list, rows.Err()
}
