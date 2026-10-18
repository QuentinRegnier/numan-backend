package two_factor_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
)

// ############################################################################
// # SERVICE : LISTER LES PASSKEYS D'UN UTILISATEUR
// ############################################################################

// GetUserPasskeys retourne la liste des Passkeys enregistrées par l'utilisateur via la cascade L1 -> L2 -> L3.
func GetUserPasskeys(ctx context.Context, userID int64) (two_factor_models.GetPasskeysOutput, error) {
	passkeys, err := object_cache_service.GetUserPasskeysCascade(ctx, userID)
	if err != nil {
		return two_factor_models.GetPasskeysOutput{}, err
	}

	views := make([]two_factor_models.PasskeyView, 0, len(passkeys))
	for _, pk := range passkeys {
		views = append(views, two_factor_models.PasskeyView{
			ID:           pk.ID,
			CredentialID: pk.CredentialID,
			DeviceName:   pk.DeviceName,
			Transports:   pk.Transports,
			CreatedAt:    pk.CreatedAt,
			UpdatedAt:    pk.UpdatedAt,
		})
	}

	return two_factor_models.GetPasskeysOutput{Passkeys: views}, nil
}
