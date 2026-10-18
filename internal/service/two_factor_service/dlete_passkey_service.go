package two_factor_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// DeletePasskey révoque une Passkey appartenant à l'utilisateur et ajuste son bitmask s'il n'en reste plus.
func DeletePasskey(ctx context.Context, userID int64, input two_factor_models.DeletePasskeyInput) error {
	passkeys, err := object_cache_service.GetUserPasskeysCascade(ctx, userID)
	if err != nil {
		return err
	}

	var target *two_factor_models.PasskeyPayload
	for i := range passkeys {
		if passkeys[i].ID == input.PasskeyID && passkeys[i].UserID == userID {
			target = &passkeys[i]
			break
		}
	}

	if target == nil {
		return numan_error.NewNotFound(numan_error.CodeNotFound, "Passkey introuvable sur votre compte.", nil)
	}

	_ = object_cache_service.DeletePasskeyFromObjectCache(ctx, userID, target.ID, target.CredentialID)
	if errQueue := redis.EnqueueDB(ctx, target.ID, userID, redis.EntityPasskey, redis.ActionDelete, *target, redis.TargetAll); errQueue != nil {
		return numan_error.NewInternal(errQueue)
	}

	// S'il s'agissait de la toute dernière Passkey de l'utilisateur, on retire le bit Passkey
	if len(passkeys) <= 1 {
		if userPayload, errUser := loadUserByIDCascade(ctx, userID); errUser == nil && userPayload.ID != 0 {
			userPayload.TwoFactorMethod &^= variables.TwoFactorBitPasskey
			if userPayload.TwoFactorMethod == 0 {
				userPayload.TwoFactorEnabled = false
			}
			userPayload.UpdatedAt = domain.NowMillis()
			_ = redis.EnqueueDB(ctx, userPayload.ID, userPayload.ID, redis.EntityUser, redis.ActionUpdate, userPayload, redis.TargetAll)
		}
	}

	return nil
}
