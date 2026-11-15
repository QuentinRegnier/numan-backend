package relation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES ABONNÉS (FOLLOWERS)
// ############################################################################

// GetFollows récupère la liste des abonnés d'un utilisateur cible.
func GetFollows(ctx context.Context, callerID int64, input relation_models.GetFollowersInput) (relation_models.GetFollowersOutput, error) {
	if callerID <= 0 {
		return relation_models.GetFollowersOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return relation_models.GetFollowersOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// On cherche les relations entrantes (incoming) de type Abonnement.
	followersViews, errFetch := fetchRelationsHydrated(ctx, callerID, input.TargetID, variables.RelationStateFollow, "incoming", input.Limit, input.Offset)
	if errFetch != nil {
		return relation_models.GetFollowersOutput{}, numan_error.NewInternal()
	}

	return relation_models.GetFollowersOutput{
		Users: followersViews,
	}, nil
}
