package auth_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉVOCATION DE SESSION À DISTANCE
// ############################################################################

// RevokeSession vérifie la propriété et détruit une session active spécifique.
func RevokeSession(ctx context.Context, callerID int64, targetSessionID int64) error {
	var sessionPayload auth_models.SessionsPayload

	// ── ÉTAPE 1 : TENTATIVE L1 (RAM) ─────────────────────────────────────────
	// On récupère la session pour vérifier à qui elle appartient et obtenir son FirebaseInstallationID
	errCache := redis.Sessions.GetObject(ctx, targetSessionID, &sessionPayload)

	if errCache != nil || sessionPayload.ID == 0 {
		// FALLBACK L3 : Si la session n'est plus en RAM, on interroge PostgreSQL (Source de Vérité)
		sessionPg, errPg := postgres.FuncLoadSession(ctx, targetSessionID, callerID, "", "")
		if errPg != nil {
			// Erreur BDD -> On loggue en interne et on renvoie une 500 propre au client
			return numan_error.NewInternal()
		}
		if sessionPg.ID == 0 {
			return numan_error.NewNotFound(numan_error.CodeNotFound, "La session est introuvable ou a déjà été révoquée.", nil)
		}
		sessionPayload = sessionPg
	}

	// ── ÉTAPE 2 : SÉCURITÉ ABSOLUE (Vérification de la Propriété) ──────────
	if sessionPayload.UserID != callerID {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous n'êtes pas le propriétaire de cette session.", nil)
	}

	// ── ÉTAPE 3 : PURGE DU CACHE L1 (Object + Index) ───────────────────────
	err := cache_service.DeleteSessionFromCache(ctx, targetSessionID, sessionPayload.UserID, sessionPayload.FirebaseInstallationID)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Impossible de supprimer la session du cache")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 4 : DÉCONNEXION TEMPS RÉEL (WebSockets) ──────────────────────
	// Le client recevra l'événement et fermera son application ou redirigera vers le login
	err = realtime_service.DistributeToUsers(ctx, variables.NotificationSessionRevoked, map[string]int64{"session_id": targetSessionID}, []int64{callerID})
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Impossible de distribuer la notification de révocation de session")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (Write-Behind vers BDD) ───────────
	minimalPayloadForDeletion := auth_models.SessionsPayload{ID: targetSessionID, UserID: callerID}

	errQueue := redis.EnqueueDB(ctx, targetSessionID, callerID, redis.EntitySession, redis.ActionDelete, minimalPayloadForDeletion, redis.TargetAll)
	if errQueue != nil {
		// L'action est cruciale. Si la mise en file échoue, on retourne une erreur interne (500).
		return numan_error.NewInternal()
	}

	return nil
}
