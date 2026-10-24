package cache_service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// SavePreAuthSession stocke une PreAuthSession en RAM (L1) via l'Object Cache.
func SavePreAuthSession(ctx context.Context, session auth_models.PreAuthSession) error {
	if err := redis.PreAuth.SetObject(ctx, session.Token, session); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de sauvegarde de la session de pré-authentification",
			err,
		)
	}
	return nil
}

// GetPreAuthSession charge et désérialise une PreAuthSession depuis l'Object Cache L1.
func GetPreAuthSession(ctx context.Context, token string) (auth_models.PreAuthSession, error) {
	var session auth_models.PreAuthSession

	if err := redis.PreAuth.GetObject(ctx, token, &session); err != nil {
		return auth_models.PreAuthSession{}, numan_error.NewUnauthorized(numan_error.CodeTwoFactorExpired, "La session de vérification a expiré ou est introuvable.", err)
	}

	if session.Attempts >= variables.TwoFactorMaxAttempts {
		if errDel := DeletePreAuthSession(ctx, token); errDel != nil {
			numan_log.Warn(ctx).Err(errDel).Action(numan_log.ActionEvict).Msg("Échec de suppression de la session de pré-authentification après dépassement des tentatives")
		}
		return auth_models.PreAuthSession{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Trop de tentatives échouées. Veuillez recommencer.", nil)
	}

	return session, nil
}

// IncrementPreAuthAttempts incrémente le compteur d'essais et supprime la session si la limite est atteinte.
func IncrementPreAuthAttempts(ctx context.Context, session auth_models.PreAuthSession) error {
	session.Attempts++
	if session.Attempts >= variables.TwoFactorMaxAttempts {
		if errDel := DeletePreAuthSession(ctx, session.Token); errDel != nil {
			numan_log.Warn(ctx).Err(errDel).Action(numan_log.ActionEvict).Msg("Échec de suppression de la session de pré-authentification révoquée")
		}
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Trop de tentatives échouées. Session révoquée.", nil)
	}

	if err := redis.PreAuth.SetObject(ctx, session.Token, session); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de mise à jour de la session de pré-authentification",
			err,
		)
	}
	return nil
}

// DeletePreAuthSession supprime immédiatement la session temporaire 2FA de l'Object Cache L1.
func DeletePreAuthSession(ctx context.Context, token string) error {
	if err := redis.PreAuth.DeleteObject(ctx, token); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de suppression de la session de pré-authentification",
			err,
		)
	}
	return nil
}

// ConsumeTOTPCode empêche le rejeu d'un même code TOTP dans sa fenêtre de validité de 90s.
func ConsumeTOTPCode(ctx context.Context, userID int64, code string) (bool, error) {
	replayID := fmt.Sprintf("totp_replay:%d:%s", userID, code)
	ok, err := redis.PreAuth.SetNX(ctx, replayID, 1, variables.WebAuthnTimeoutMs)
	if err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Échec de la vérification anti-rejeu du code TOTP",
			err,
		)
	}
	return ok, nil
}
