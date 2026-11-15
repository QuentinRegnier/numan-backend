package relation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES AMIS
// ############################################################################

// GetFriends récupère la liste des amis d'un utilisateur cible.
func GetFriends(ctx context.Context, callerID int64, input relation_models.GetFriendsInput) (relation_models.GetFriendsOutput, error) {
	if callerID <= 0 {
		return relation_models.GetFriendsOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return relation_models.GetFriendsOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// On cherche les relations entrantes (incoming) de type Ami.
	friendsViews, errFetch := fetchRelationsHydrated(ctx, callerID, input.TargetID, variables.RelationStateFriend, "incoming", input.Limit, input.Offset)
	if errFetch != nil {
		return relation_models.GetFriendsOutput{}, numan_error.NewInternal()
	}

	return relation_models.GetFriendsOutput{
		Users: friendsViews,
	}, nil
}
