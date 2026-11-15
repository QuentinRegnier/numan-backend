package auth_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
)

// ############################################################################
// # SERVICE : LISTE DES SESSIONS ACTIVES
// ############################################################################

// GetUserSessions récupère les sessions actives de l'utilisateur.
// Elle les expurge de toute donnée critique (Tokens, Secrets) avant envoi au front-end.
func GetUserSessions(ctx context.Context, userID int64) ([]auth_models.SessionView, error) {
	if userID <= 0 {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}

	// ── ÉTAPE 1 : LECTURE DEPUIS LA SOURCE DE VÉRITÉ (PostgreSQL L3) ────────
	// On interroge directement la vue L3 car on a besoin d'une vue d'ensemble complète et fiable.
	activeSessions, errPg := postgres.FuncLoadUserSessionsView(ctx, userID)
	if errPg != nil {
		return nil, numan_error.NewInternal() // Protège l'erreur SQL
	}

	// ── ÉTAPE 2 : CONVERSION ET NETTOYAGE SÉCURISÉ (MAPPING DTO) ────────────
	var publicSessionViews []auth_models.SessionView

	for _, session := range activeSessions {
		publicSessionViews = append(publicSessionViews, auth_models.SessionView{
			ID:         session.ID,
			DeviceInfo: session.DeviceInfo,
			IPHistory:  session.IPHistory,
			CreatedAt:  session.CreatedAt,
			ExpiresAt:  session.ExpiresAt,
		})
	}

	// Prévention stricte du retour 'null' en JSON (Tableau vide favorisé)
	if publicSessionViews == nil {
		publicSessionViews = make([]auth_models.SessionView, 0)
	}

	return publicSessionViews, nil
}
