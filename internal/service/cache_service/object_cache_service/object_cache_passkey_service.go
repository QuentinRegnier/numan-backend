package object_cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
)

func passkeyUserKey(userID int64) string {
	return fmt.Sprintf("user:%d", userID)
}

func passkeyCredKey(credentialID string) string {
	return fmt.Sprintf("cred:%s", credentialID)
}

// SetPasskeyInObjectCache met en cache une Passkey en L1 et invalide la liste agrégée de l'utilisateur pour forcer son rafraîchissement propre.
func SetPasskeyInObjectCache(ctx context.Context, passkey two_factor_models.PasskeyPayload) error {
	if err := redis.Passkeys.SetObject(ctx, passkey.ID, passkey); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de sauvegarder la Passkey dans l'Object Cache.", err)
	}
	if err := redis.Passkeys.SetObject(ctx, passkeyCredKey(passkey.CredentialID), passkey); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de sauvegarder la Passkey dans l'Object Cache.", err)
	}
	if err := redis.Passkeys.DeleteObject(ctx, passkeyUserKey(passkey.UserID)); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de sauvegarder la Passkey dans l'Object Cache.", err)
	}
	return nil
}

// DeletePasskeyFromObjectCache évince une Passkey de toutes ses clés d'indexation L1.
func DeletePasskeyFromObjectCache(ctx context.Context, userID int64, passkeyID int64, credentialID string) error {
	if err := redis.Passkeys.DeleteObject(ctx, passkeyID); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de supprimer la Passkey de l'Object Cache.", err)
	}
	if credentialID != "" {
		if err := redis.Passkeys.DeleteObject(ctx, passkeyCredKey(credentialID)); err != nil {
			return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de supprimer la Passkey de l'Object Cache.", err)
		}
	}
	if err := redis.Passkeys.DeleteObject(ctx, passkeyUserKey(userID)); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de supprimer la Passkey de l'Object Cache.", err)
	}
	return nil
}

// GetUserPasskeysCascade récupère les Passkeys d'un utilisateur via la cascade L1 -> L2 -> L3 avec auto-guérison.
func GetUserPasskeysCascade(ctx context.Context, userID int64) ([]two_factor_models.PasskeyPayload, error) {
	// 1. TENTATIVE L1 (Redis Object Cache)
	var cachedList []two_factor_models.PasskeyPayload
	if errL1 := redis.Passkeys.GetObject(ctx, passkeyUserKey(userID), &cachedList); errL1 == nil && len(cachedList) > 0 {
		return cachedList, nil
	} else if errL1 != nil && !errors.Is(errL1, goredis.Nil) {
		numan_log.Warn(ctx).Err(errL1).Int64("user_id", userID).Msg("Erreur L1 lors de la lecture des Passkeys utilisateur")
	}

	// 2. TENTATIVE L2 (MongoDB Warm Storage)
	if listL2, errL2 := mongo.MongoLoadUserPasskeys(ctx, userID); errL2 != nil {
		numan_log.Warn(ctx).Err(errL2).Int64("user_id", userID).Msg("Échec L2 lors du chargement des Passkeys utilisateur")
	} else if len(listL2) > 0 {
		if errSet := redis.Passkeys.SetObject(ctx, passkeyUserKey(userID), listL2); errSet != nil {
			numan_log.Warn(ctx).Err(errSet).Int64("user_id", userID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la liste des Passkeys utilisateur depuis L2")
		}
		for _, p := range listL2 {
			if errSet := redis.Passkeys.SetObject(ctx, p.ID, p); errSet != nil {
				numan_log.Warn(ctx).Err(errSet).Entity("passkey", p.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par ID depuis L2")
			}
			if errSet := redis.Passkeys.SetObject(ctx, passkeyCredKey(p.CredentialID), p); errSet != nil {
				numan_log.Warn(ctx).Err(errSet).Entity("passkey", p.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par CredentialID depuis L2")
			}
		}
		return listL2, nil
	}

	// 3. FALLBACK L3 (PostgreSQL Cold Storage)
	listL3, errL3 := postgres.FuncGetUserPasskeys(ctx, userID)
	if errL3 != nil {
		// Error already wrapped by FuncGetUserPasskeys; propagate it without wrapping again.
		return nil, errL3
	}

	if len(listL3) > 0 {
		// Auto-guérison L3 -> L1 (Synchrone)
		if errSet := redis.Passkeys.SetObject(ctx, passkeyUserKey(userID), listL3); errSet != nil {
			numan_log.Warn(ctx).Err(errSet).Int64("user_id", userID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la liste des Passkeys utilisateur depuis L3")
		}
		for _, p := range listL3 {
			if errSet := redis.Passkeys.SetObject(ctx, p.ID, p); errSet != nil {
				numan_log.Warn(ctx).Err(errSet).Entity("passkey", p.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par ID depuis L3")
			}
			if errSet := redis.Passkeys.SetObject(ctx, passkeyCredKey(p.CredentialID), p); errSet != nil {
				numan_log.Warn(ctx).Err(errSet).Entity("passkey", p.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par CredentialID depuis L3")
			}

			// Auto-guérison L3 -> L2 (Asynchrone via EnqueueDB)
			go func(pk two_factor_models.PasskeyPayload) {
				bgCtx := context.Background()
				if err := redis.EnqueueDB(bgCtx, pk.ID, pk.UserID, redis.EntityPasskey, redis.ActionUpdate, pk, redis.TargetMongo); err != nil {
					numan_log.Warn(bgCtx).Err(err).Entity("passkey", pk.ID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 de la Passkey")
				}
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
	} else if errL1 != nil && !errors.Is(errL1, goredis.Nil) {
		numan_log.Warn(ctx).Err(errL1).Msg("Erreur L1 lors de la lecture de la Passkey par CredentialID")
	}

	// 2. TENTATIVE L2 (MongoDB Warm Storage)
	if pL2, errL2 := mongo.MongoGetPasskeyByCredentialID(ctx, credentialID); errL2 == nil && pL2.ID != 0 {
		if errSet := redis.Passkeys.SetObject(ctx, pL2.ID, pL2); errSet != nil {
			numan_log.Warn(ctx).Err(errSet).Entity("passkey", pL2.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par ID depuis L2")
		}
		if errSet := redis.Passkeys.SetObject(ctx, passkeyCredKey(pL2.CredentialID), pL2); errSet != nil {
			numan_log.Warn(ctx).Err(errSet).Entity("passkey", pL2.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par CredentialID depuis L2")
		}
		return pL2, nil
	}

	// 3. FALLBACK L3 (PostgreSQL Cold Storage)
	pL3, errL3 := postgres.FuncGetPasskeyByCredentialID(ctx, credentialID)
	if errL3 != nil {
		// Error already wrapped by FuncGetPasskeyByCredentialID; propagate it without wrapping again.
		return two_factor_models.PasskeyPayload{}, errL3
	}
	if pL3.ID == 0 {
		return two_factor_models.PasskeyPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Passkey introuvable.", nil)
	}

	// Auto-guérison L3 -> L1
	if errSet := redis.Passkeys.SetObject(ctx, pL3.ID, pL3); errSet != nil {
		numan_log.Warn(ctx).Err(errSet).Entity("passkey", pL3.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par ID depuis L3")
	}
	if errSet := redis.Passkeys.SetObject(ctx, passkeyCredKey(pL3.CredentialID), pL3); errSet != nil {
		numan_log.Warn(ctx).Err(errSet).Entity("passkey", pL3.ID).Action(numan_log.ActionHydrate).Msg("Échec de la mise en cache L1 de la Passkey par CredentialID depuis L3")
	}

	// Auto-guérison L3 -> L2
	go func(pk two_factor_models.PasskeyPayload) {
		bgCtx := context.Background()
		if err := redis.EnqueueDB(bgCtx, pk.ID, pk.UserID, redis.EntityPasskey, redis.ActionUpdate, pk, redis.TargetMongo); err != nil {
			numan_log.Warn(bgCtx).Err(err).Entity("passkey", pk.ID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 de la Passkey")
		}
	}(pL3)

	return pL3, nil
}
