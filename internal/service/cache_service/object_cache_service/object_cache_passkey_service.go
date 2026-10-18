package object_cache_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func passkeyUserKey(userID int64) string {
	return fmt.Sprintf("user:%d", userID)
}

func passkeyCredKey(credentialID string) string {
	return fmt.Sprintf("cred:%s", credentialID)
}

// SetPasskeyInObjectCache met en cache une Passkey en L1 et invalide la liste agrégée de l'utilisateur pour forcer son rafraîchissement propre.
func SetPasskeyInObjectCache(ctx context.Context, passkey two_factor_models.PasskeyPayload) error {
	_ = redis.Passkeys.SetObject(ctx, passkey.ID, passkey)
	_ = redis.Passkeys.SetObject(ctx, passkeyCredKey(passkey.CredentialID), passkey)
	_ = redis.Passkeys.DeleteObject(ctx, passkeyUserKey(passkey.UserID))
	return nil
}

// DeletePasskeyFromObjectCache évince une Passkey de toutes ses clés d'indexation L1.
func DeletePasskeyFromObjectCache(ctx context.Context, userID int64, passkeyID int64, credentialID string) error {
	_ = redis.Passkeys.DeleteObject(ctx, passkeyID)
	if credentialID != "" {
		_ = redis.Passkeys.DeleteObject(ctx, passkeyCredKey(credentialID))
	}
	_ = redis.Passkeys.DeleteObject(ctx, passkeyUserKey(userID))
	return nil
}

// GetUserPasskeysCascade récupère les Passkeys d'un utilisateur via la cascade L1 -> L2 -> L3 avec auto-guérison.
func GetUserPasskeysCascade(ctx context.Context, userID int64) ([]two_factor_models.PasskeyPayload, error) {
	// 1. TENTATIVE L1 (Redis Object Cache)
	var cachedList []two_factor_models.PasskeyPayload
	if errL1 := redis.Passkeys.GetObject(ctx, passkeyUserKey(userID), &cachedList); errL1 == nil && len(cachedList) > 0 {
		return cachedList, nil
	}

	// 2. TENTATIVE L2 (MongoDB Warm Storage)
	if listL2, errL2 := mongo.MongoLoadUserPasskeys(ctx, userID); errL2 == nil && len(listL2) > 0 {
		_ = redis.Passkeys.SetObject(ctx, passkeyUserKey(userID), listL2)
		for _, p := range listL2 {
			_ = redis.Passkeys.SetObject(ctx, p.ID, p)
			_ = redis.Passkeys.SetObject(ctx, passkeyCredKey(p.CredentialID), p)
		}
		return listL2, nil
	}

	// 3. FALLBACK L3 (PostgreSQL Cold Storage)
	listL3, errL3 := postgres.FuncGetUserPasskeys(ctx, userID)
	if errL3 != nil {
		return nil, numan_error.NewInternal(errL3)
	}

	if len(listL3) > 0 {
		// Auto-guérison L3 -> L1 (Synchrone)
		_ = redis.Passkeys.SetObject(ctx, passkeyUserKey(userID), listL3)
		for _, p := range listL3 {
			_ = redis.Passkeys.SetObject(ctx, p.ID, p)
			_ = redis.Passkeys.SetObject(ctx, passkeyCredKey(p.CredentialID), p)

			// Auto-guérison L3 -> L2 (Asynchrone via EnqueueDB)
			go func(pk two_factor_models.PasskeyPayload) {
				_ = redis.EnqueueDB(context.Background(), pk.ID, pk.UserID, redis.EntityPasskey, redis.ActionUpdate, pk, redis.TargetMongo)
			}(p)
		}
	}

	return listL3, nil
}

// GetPasskeyByCredentialIDCascade récupère une Passkey par son CredentialID via la cascade L1 -> L2 -> L3.
func GetPasskeyByCredentialIDCascade(ctx context.Context, credentialID string) (two_factor_models.PasskeyPayload, error) {
	// 1. TENTATIVE L1 (Redis Object Cache)
	var cached two_factor_models.PasskeyPayload
	if errL1 := redis.Passkeys.GetObject(ctx, passkeyCredKey(credentialID), &cached); errL1 == nil && cached.ID != 0 {
		return cached, nil
	}

	// 2. TENTATIVE L2 (MongoDB Warm Storage)
	if pL2, errL2 := mongo.MongoGetPasskeyByCredentialID(ctx, credentialID); errL2 == nil && pL2.ID != 0 {
		_ = redis.Passkeys.SetObject(ctx, pL2.ID, pL2)
		_ = redis.Passkeys.SetObject(ctx, passkeyCredKey(pL2.CredentialID), pL2)
		return pL2, nil
	}

	// 3. FALLBACK L3 (PostgreSQL Cold Storage)
	pL3, errL3 := postgres.FuncGetPasskeyByCredentialID(ctx, credentialID)
	if errL3 != nil {
		return two_factor_models.PasskeyPayload{}, numan_error.NewInternal(errL3)
	}
	if pL3.ID == 0 {
		return two_factor_models.PasskeyPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Passkey introuvable.", nil)
	}

	// Auto-guérison L3 -> L1
	_ = redis.Passkeys.SetObject(ctx, pL3.ID, pL3)
	_ = redis.Passkeys.SetObject(ctx, passkeyCredKey(pL3.CredentialID), pL3)

	// Auto-guérison L3 -> L2
	go func(pk two_factor_models.PasskeyPayload) {
		_ = redis.EnqueueDB(context.Background(), pk.ID, pk.UserID, redis.EntityPasskey, redis.ActionUpdate, pk, redis.TargetMongo)
	}(pL3)

	return pL3, nil
}
