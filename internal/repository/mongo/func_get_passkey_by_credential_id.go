package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoGetPasskeyByCredentialID récupère une Passkey par son CredentialID depuis le Warm Storage L2.
func MongoGetPasskeyByCredentialID(ctx context.Context, credentialID string) (two_factor_models.PasskeyPayload, error) {
	if Passkeys == nil {
		return two_factor_models.PasskeyPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Passkeys' is not initialized"),
		)
	}

	if credentialID == "" {
		return two_factor_models.PasskeyPayload{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Passkey introuvable.",
			errors.New("credentialID cannot be empty"),
		)
	}

	filter := map[string]any{"credential_id": credentialID}
	docs, err := Passkeys.GetPaginated(filter, nil, 0, 1)
	if err != nil {
		return two_factor_models.PasskeyPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer la passkey.",
			err,
		)
	}

	if len(docs) == 0 {
		return two_factor_models.PasskeyPayload{}, numan_error.NewNotFound(
			numan_error.CodeNotFound,
			"Passkey introuvable.",
			fmt.Errorf("passkey with credential ID '%s' was not found in Warm Storage (L2)", credentialID),
		)
	}

	var p two_factor_models.PasskeyPayload
	if errStruct := pkg.ToStruct(docs[0], &p); errStruct != nil {
		return two_factor_models.PasskeyPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger la passkey.",
			errStruct,
		)
	}

	return p, nil
}
