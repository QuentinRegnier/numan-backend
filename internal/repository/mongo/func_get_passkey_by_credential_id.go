package mongo

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoGetPasskeyByCredentialID récupère une Passkey par son CredentialID depuis le Warm Storage L2.
func MongoGetPasskeyByCredentialID(ctx context.Context, credentialID string) (two_factor_models.PasskeyPayload, error) {
	if Passkeys == nil || credentialID == "" {
		return two_factor_models.PasskeyPayload{}, nil
	}

	filter := map[string]any{"credential_id": credentialID}
	docs, err := Passkeys.GetPaginated(filter, nil, 0, 1)
	if err != nil || len(docs) == 0 {
		return two_factor_models.PasskeyPayload{}, err
	}

	var p two_factor_models.PasskeyPayload
	if errStruct := pkg.ToStruct(docs[0], &p); errStruct != nil {
		return two_factor_models.PasskeyPayload{}, errStruct
	}
	return p, nil
}
