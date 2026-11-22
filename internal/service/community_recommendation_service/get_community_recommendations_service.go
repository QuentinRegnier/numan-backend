package community_recommendation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetCommunityRecommendations retourne une liste paginée, personnalisée par affinité
// amis de degré 1 et enrichie des slots épinglés administrativement pour la langue cible.
//
// Contrat strict : aucun champ is_joined n'est calculé ni retourné côté backend.
func GetCommunityRecommendations(
	ctx context.Context,
	callerID int64,
	input community_recommendation_models.GetCommunityRecommendationsInput,
) (community_recommendation_models.GetCommunityRecommendationsOutput, error) {
	if callerID <= 0 {
		return community_recommendation_models.GetCommunityRecommendationsOutput{}, numan_error.NewUnauthorized(
			numan_error.CodeUserIsNotIdentified,
			"Utilisateur non identifié.",
			nil,
		)
	}
	if err := input.Validate(); err != nil {
		return community_recommendation_models.GetCommunityRecommendationsOutput{}, err
	}

	// 1. Résolution de la langue cible (paramètre explicite -> UserSettings -> défaut "fr")
	resolvedLang := resolveTargetCommunityLang(ctx, callerID, input.Lang)

	// 2. Lecture du ZSET global précalculé (L1 Redis)
	rankedEntries, errRanked := cache_service.GetRankedCommunitiesByLang(ctx, resolvedLang)
	if errRanked != nil {
		return community_recommendation_models.GetCommunityRecommendationsOutput{}, errRanked
	}

	// 3. Récupération des amis directs (degré 1) capés + leurs communautés publiques en 2 RTT max
	var friendCounts map[int64]int
	friendIDs, errFriends := cache_service.GetDirectFriendIDsCapped(ctx, callerID, variables.CommunityRecoFriendEvalCap)
	if errFriends != nil {
		numan_log.Warn(ctx).Err(errFriends).Int64("user_id", callerID).Msg("Échec lecture amis directs pour recommandation communautés (dégradation gracieuse)")
	} else if len(friendIDs) > 0 {
		fc, errComms := cache_service.GetFriendsPublicCommunities(ctx, friendIDs)
		if errComms != nil {
			numan_log.Warn(ctx).Err(errComms).Int64("user_id", callerID).Msg("Échec lecture user:public_comms amis (dégradation gracieuse)")
		} else {
			friendCounts = fc
		}
	}

	// 4. Transformation vers les candidats du moteur pur & boost amis
	rankedCandidates := make([]community_recommendation_models.RankedCandidate, 0, len(rankedEntries))
	rankedSet := make(map[int64]struct{}, len(rankedEntries))
	for _, entry := range rankedEntries {
		rankedCandidates = append(rankedCandidates, community_recommendation_models.RankedCandidate{
			ConversationID: entry.ConversationID,
			GlobalScore:    entry.Score,
		})
		rankedSet[entry.ConversationID] = struct{}{}
	}

	personalized := ApplyFriendBoost(rankedCandidates, friendCounts, variables.CommunityRecoFriendBoostGamma)

	// 5. Lecture des épinglages administratifs de la langue et injection aux slots exacts
	pins, errPins := cache_service.GetCommunityPinsByLang(ctx, resolvedLang)
	if errPins != nil {
		numan_log.Warn(ctx).Err(errPins).Str("lang", resolvedLang).Msg("Échec lecture des pins communautaires (dégradation gracieuse)")
		pins = nil
	}

	// Support des communautés fraîchement épinglées pas encore présentes dans le ZSET
	var extraPinned map[int64]community_recommendation_models.PersonalizedCandidate
	if len(pins) > 0 {
		var missingPinnedIDs []int64
		for _, convID := range pins {
			if _, ok := rankedSet[convID]; !ok {
				missingPinnedIDs = append(missingPinnedIDs, convID)
			}
		}
		if len(missingPinnedIDs) > 0 {
			extraMetas := loadConvMetasWithFallback(ctx, missingPinnedIDs)
			for _, convID := range missingPinnedIDs {
				meta, ok := extraMetas[convID]
				if !ok || meta.Type != variables.ConversationTypeCommunityPub {
					continue
				}
				if meta.Lang != "" && meta.Lang != resolvedLang {
					continue
				}
				if extraPinned == nil {
					extraPinned = make(map[int64]community_recommendation_models.PersonalizedCandidate, len(missingPinnedIDs))
				}
				fc := 0
				if friendCounts != nil {
					fc = friendCounts[convID]
				}
				extraPinned[convID] = community_recommendation_models.PersonalizedCandidate{
					ConversationID: convID,
					FriendCount:    fc,
				}
			}
		}
	}

	assembled := InjectPinnedSlots(personalized, pins, extraPinned)
	total := len(assembled)

	// 6. Pagination APRÈS assemblage complet (aucun doublon aux frontières de page)
	page, hasMore := PaginateCandidates(assembled, input.EffectiveOffset(), input.EffectiveLimit())
	if len(page) == 0 {
		return community_recommendation_models.GetCommunityRecommendationsOutput{
			Lang:        resolvedLang,
			Total:       total,
			HasMore:     hasMore,
			Communities: []community_recommendation_models.CommunityRecommendationItem{},
		}, nil
	}

	// 7. Hydratation batch L1 uniquement sur la page courante (<= 50 items)
	pageIDs := make([]int64, len(page))
	for i, c := range page {
		pageIDs[i] = c.ConversationID
	}

	metas := loadConvMetasWithFallback(ctx, pageIDs)
	memberCounts, errCounts := cache_service.GetCommunityMemberCountsBatch(ctx, pageIDs)
	if errCounts != nil {
		numan_log.Warn(ctx).Err(errCounts).Msg("Échec lecture batch des compteurs de membres communautaires")
		memberCounts = make(map[int64]int)
	}

	items := make([]community_recommendation_models.CommunityRecommendationItem, 0, len(page))
	for _, cand := range page {
		meta, ok := metas[cand.ConversationID]
		if !ok || meta.ID == 0 || meta.Type != variables.ConversationTypeCommunityPub {
			continue
		}

		langValue := meta.Lang
		if langValue == "" {
			langValue = resolvedLang
		}

		items = append(items, community_recommendation_models.CommunityRecommendationItem{
			ID:            meta.ID,
			Name:          meta.Title,
			Description:   meta.Description,
			AvatarMediaID: meta.AvatarID,
			Lang:          langValue,
			Mode:          string(lite_models.ResolveCommunityMode(meta)),
			MemberCount:   memberCounts[meta.ID],
			IsPartner:     meta.IsPartner,
			IsPinned:      cand.IsPinned,
			PinSlot:       cand.PinSlot,
			FriendsCount:  cand.FriendCount,
		})
	}

	return community_recommendation_models.GetCommunityRecommendationsOutput{
		Lang:        resolvedLang,
		Total:       total,
		HasMore:     hasMore,
		Communities: items,
	}, nil
}
