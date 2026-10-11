package auth_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

// ############################################################################
// # SERVICE : DÉCONNEXION (LOGOUT)
// ############################################################################

// Logout déconnecte l'appareil courant en utilisant le firebaseInstallationID du JWT.
// L'opération est idempotente : si la session n'existe plus, on renvoie une réussite silencieuse.
func Logout(ctx context.Context, callerID int64, firebaseInstallationID string) error {
	if firebaseInstallationID == "" {
		return nil // Rien à déconnecter, requête ignorée gracieusement.
	}

	var sessionPayload auth_models.SessionsPayload

	// ── ÉTAPE 1 : RÉCUPÉRATION DE LA SESSION (CASCADE L1 -> L2 -> L3) ───────

	// TENTATIVE L1 (RAM)
	sessionPayload, err := cache_service.LoadSessionFromCache(ctx, callerID, firebaseInstallationID, "")
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Échec du chargement de la session depuis le cache L1")
		return numan_error.NewInternal()
	}

	if sessionPayload.ID == 0 {
		// FALLBACK L2 (Mongo - Warm Storage)
		sessionPayload, err = mongo.MongoLoadSession(ctx, callerID, firebaseInstallationID, "", "")
		if err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Échec du chargement de la session depuis Mongo L2")
			return numan_error.NewInternal()
		}

		if sessionPayload.ID == 0 {
			// FALLBACK L3 (Postgres - Cold Storage)
			sessionPg, errPg := postgres.FuncLoadSession(ctx, -1, callerID, firebaseInstallationID, "")
			if errPg != nil {
				// On loggue l'erreur interne, mais on ne la remonte pas au client.
				numan_log.Error(ctx).Err(errPg).Msg("Erreur lors du fallback L3 pour le Logout")
				return nil
			}

			if sessionPg.ID == 0 {
				// Idempotence absolue : Si introuvable partout, on considère l'utilisateur déjà déconnecté
				return nil
			}
			sessionPayload = sessionPg
		}
	}

	// ── ÉTAPE 2 : DESTRUCTION LOCALE EN RAM (L1) ────────────────────────────
	// On passe par le Service Cache pour respecter le DDD et supprimer à la fois l'objet et ses index.
	err = cache_service.DeleteSessionFromCache(ctx, sessionPayload.ID, callerID, firebaseInstallationID)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Échec de la suppression de la session depuis le cache L1")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 3 : PERSISTANCE ASYNCHRONE (Write-Behind) ─────────────────────
	// On envoie la demande de suppression (Hard Delete) aux workers pour nettoyer Mongo et Postgres.
	minimalPayload := auth_models.SessionsPayload{ID: sessionPayload.ID, UserID: callerID}

	errQueue := redis.EnqueueDB(ctx, sessionPayload.ID, callerID, redis.EntitySession, redis.ActionDelete, minimalPayload, redis.TargetAll)
	if errQueue != nil {
		// Log en erreur car le worker n'a pas reçu l'ordre, la session risque de rester fantôme en L3.
		numan_log.Error(ctx).Err(errQueue).Int64("session_id", sessionPayload.ID).Msg("Échec de la mise en file d'attente du Logout")
	}

	return nil
}
