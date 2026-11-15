package search_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RECHERCHE DE COMMUNAUTÉS
// ############################################################################

// searchCommunities orchestre la recherche ultrarapide de communautés publiques
// via le Speed Cache (O(log(N))) et génère les URL signées pour les avatars.
func searchCommunities(ctx context.Context, callerID int64, input search_models.CommunitySearchInput) (search_models.CommunitySearchOutput, error) {
	if err := input.Validate(); err != nil {
		return search_models.CommunitySearchOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = variables.DefaultGlobalSearchLimit
		if input.Limit <= 0 {
			input.Limit = 20
		}
	}

	// ── ÉTAPE 1 : RÉSOLUTION DE L'INDEX DANS LE SPEED CACHE (L1) ────────────
	liteCommunitiesResults, errRedis := cache_service.SearchCommunitiesByPrefix(ctx, input.Prefix, input.Offset, input.Limit)
	if errRedis != nil {
		numan_log.Error(ctx).Err(errRedis).Str("prefix", input.Prefix).Msg("Erreur L1 lors de la recherche des communautés")
		return search_models.CommunitySearchOutput{}, numan_error.NewInternal()
	}

	if len(liteCommunitiesResults) == 0 {
		return search_models.CommunitySearchOutput{
			Communities: make([]conversation_models.CommunityLiteView, 0),
		}, nil
	}

	// ── ÉTAPE 2 : HYDRATATION EN MASSE VIA LE DOMAINE MÉDIA ─────────────────

	hydratedCommunityViews := make([]conversation_models.CommunityLiteView, 0, len(liteCommunitiesResults))

	for _, communityLite := range liteCommunitiesResults {

		var resolvedAvatar media_models.MediaView

		if communityLite.ProfilePictureID > 0 {
			if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, communityLite.ProfilePictureID, communityLite.ID, 0, callerID); errMedia == nil {
				resolvedAvatar = mediaView
			}
		}

		hydratedCommunityViews = append(hydratedCommunityViews, conversation_models.CommunityLiteView{
			ID:          communityLite.ID,
			Name:        communityLite.Name,
			Avatar:      resolvedAvatar,
			Description: communityLite.Description,
			MemberCount: communityLite.MemberCount,
		})
	}

	return search_models.CommunitySearchOutput{
		Communities: hydratedCommunityViews,
	}, nil
}
