package relation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES UTILISATEURS BLOQUÉS
// ############################################################################

// GetBlockedUsers récupère la liste des utilisateurs que l'appelant a bloqués.
func GetBlockedUsers(ctx context.Context, callerID int64, input relation_models.GetBlockedInput) (relation_models.GetBlockedOutput, error) {
	if callerID <= 0 {
		return relation_models.GetBlockedOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return relation_models.GetBlockedOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// La cible est l'appelant lui-même, on cherche les relations sortantes (outgoing) bloquées.
	blockedUsersViews, errFetch := fetchRelationsHydrated(ctx, callerID, callerID, variables.RelationStateBlocked, "outgoing", input.Limit, input.Offset)
	if errFetch != nil {
		return relation_models.GetBlockedOutput{}, numan_error.NewInternal()
	}

	return relation_models.GetBlockedOutput{
		Users: blockedUsersViews,
	}, nil
}
