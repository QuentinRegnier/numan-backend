package feed_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/feed_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetFeed orchestre la distribution, la rotation (A/B/C), l'élargissement et l'hydratation des posts.
func GetFeed(ctx context.Context, callerID int64, input feed_models.GetFeedInput) ([]post_models.GetPostOutput, int, string, error) {
	isManualRefresh := input.Force

	// ── ÉTAPE 1 : Rapatriement du Graphe Social (O(1) L1) ─────────────────
	friendIDsList, _ := cache_service.GetSpeedFriends(ctx, callerID)
	friendIDsMap := make(map[int64]bool, len(friendIDsList))
	for _, friendID := range friendIDsList {
		friendIDsMap[friendID] = true
	}

	// ── ÉTAPE 2 : Lecture de l'ADN Algorithmique (Télémétrie L1) ──────────
	profile, err := cache_service.GetTelemetryProfile(ctx, callerID)
	if err != nil || profile.ConfidenceScore == 0.0 {
		return nil, 0, "", numan_error.NewNotFound(
			numan_error.CodeTelemetrySyncRequired,
			"No trusted telemetry data available. Please synchronize the profile.",
			nil,
		)
	}

	userVector := profile.Vector
	confidenceScore := profile.ConfidenceScore

	// ── ÉTAPE 3 : Paramétrage du Distributeur et du Magasinier ────────────
	refreshOptions := algorithm_service.RefreshOptions{
		UserID:        callerID,
		LastSeenIndex: input.LastSeenIndex,
		Quotas: algorithm_service.Quotas{
			MaxCandidates: variables.TDDCandidates,
			SocialRatio:   variables.SocialRatio,
			TagRatio:      variables.TagRatio,
			GlobalRatio:   variables.GlobalRatio,
		},
		PersonalOpts: algorithm_service.PersonalizedFeedOptions{
			UserID:         callerID,
			UserVec:        userVector,
			UserConfidence: confidenceScore, // ✅ REMPLACEMENT DU PLACEHOLDER
			FriendIDs:      friendIDsMap,
			Date:           domain.NowTime(),
			Limit:          variables.TDDFeedSize,
		},
		IsForce: isManualRefresh,
	}

	// ── ÉTAPE 4 : Boucle d'hydratation sécurisée (Remplissage strict) ─────
	var hydratedFeed []post_models.GetPostOutput
	missingPostsCount := variables.FeedPageSize
	currentScrollIndex := input.LastSeenIndex

	for missingPostsCount > 0 {
		refreshOptions.LastSeenIndex = currentScrollIndex
		refreshOptions.FetchCount = missingPostsCount

		// Extraction via les Tampons Tournants A/B/C
		fetchedIDs, err := algorithm_service.HandlePullToRefresh(ctx, refreshOptions)
		if err != nil || len(fetchedIDs) == 0 {
			break // ZSET épuisé, on arrête l'hydratation
		}

		// Hydratation riche (Média HMAC, Commentaires, Pseudos, Visibilité)
		richPostsData, err := post_service.GetPosts(ctx, callerID, post_models.GetPostInput{
			PostIDs: fetchedIDs,
		})
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Échec critique lors de l'hydratation des posts du feed")
			return nil, 0, "", numan_error.NewInternal(err)
		}

		// Filtrage absolu des trous de visibilité (Soft Deletes)
		for _, postView := range richPostsData {
			if postView.Error == "" && postView.Data.ID != 0 {
				hydratedFeed = append(hydratedFeed, postView)
			}
		}

		currentScrollIndex += len(fetchedIDs)
		missingPostsCount = variables.FeedPageSize - len(hydratedFeed)
	}

	// ── ÉTAPE 5 : Rendu Client ────────────────────────────────────────────
	if len(hydratedFeed) == 0 {
		return []post_models.GetPostOutput{}, input.LastSeenIndex, "A", numan_error.NewNotFound(numan_error.CodeNotFound, "Aucun post disponible ou visible.", nil)
	}

	// Récupération de la lettre du tampon actif (A, B ou C) pour le debug client
	feedState, _ := algorithm_service.GetUserFeedState(ctx, callerID)
	return hydratedFeed, currentScrollIndex, feedState.ActiveFeed, nil
}
