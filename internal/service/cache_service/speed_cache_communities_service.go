package cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : SPEED CACHE (COMMUNAUTÉS)
// ############################################################################

// StoreCommunityLiteInSpeedCache sauvegarde un objet CommunityLiteRequest
// et met à jour l'index Lexicographique pour l'autocomplétion.
func StoreCommunityLiteInSpeedCache(ctx context.Context, communityLitePayload lite_models.CommunityLiteRequest) error {
	lexicographicValue := fmt.Sprintf("%s:%d", strings.ToLower(communityLitePayload.Name), communityLitePayload.ID)

	errZAdd := redis.CommunitiesLex.ZAdd(ctx, variables.LexicographicGlobalKey, 0, lexicographicValue)
	if errZAdd != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'indexer la communauté dans le dictionnaire du cache.",
			errZAdd,
		)
	}

	errSet := redis.SpeedCommunity.SetObject(ctx, communityLitePayload.ID, communityLitePayload)
	if errSet != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder la communauté dans le cache.",
			errSet,
		)
	}

	return nil
}

// updateCommunityMemberCountInSpeedCache met à jour le compteur de membres en RAM (O(1)).
func updateCommunityMemberCountInSpeedCache(ctx context.Context, communityID int64, delta int) {
	if delta == 0 {
		return
	}

	var communityLitePayload lite_models.CommunityLiteRequest

	// On modifie silencieusement seulement si la communauté est bien présente en L1
	errGet := redis.SpeedCommunity.GetObject(ctx, communityID, &communityLitePayload)
	if errGet != nil && !errors.Is(errGet, goredis.Nil) {
		numan_log.Warn(ctx).Err(errGet).Int64("community_id", communityID).Msg("Erreur Redis lors de la lecture de la communauté en L1")
	}
	if errGet == nil && communityLitePayload.ID != 0 {
		communityLitePayload.MemberCount += delta
		if communityLitePayload.MemberCount < 0 {
			communityLitePayload.MemberCount = 0
		}

		errSet := redis.SpeedCommunity.SetObject(ctx, communityLitePayload.ID, communityLitePayload)
		if errSet != nil {
			numan_log.Warn(ctx).Err(errSet).Int64("community_id", communityID).Msg("Échec de la mise à jour du compteur de membres en L1")
		}
	}
}

// SearchCommunitiesByPrefix recherche des communautés en O(log(N)) RAM et les réhydrate.
func SearchCommunitiesByPrefix(ctx context.Context, searchPrefix string, resultOffset, resultLimit int64) ([]lite_models.CommunityLiteRequest, error) {

	// 1. Recherche ultra-rapide dans l'index lexicographique
	lexicographicResultsList, errLex := redis.CommunitiesLex.ZRangeByLex(ctx, variables.LexicographicGlobalKey, strings.ToLower(searchPrefix), resultOffset, resultLimit)
	if errLex != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de rechercher les communautés dans le cache.",
			errLex,
		)
	}

	if len(lexicographicResultsList) == 0 {
		return []lite_models.CommunityLiteRequest{}, nil
	}

	// 2. Extraction des IDs
	var extractedCommunityIDs []int64
	for _, lexResultString := range lexicographicResultsList {
		// Le format stocké est "nom:id"
		lexParts := strings.Split(lexResultString, ":")
		if len(lexParts) == 2 {
			if parsedID := pkg.ParseInt64(lexParts[1]); parsedID != 0 {
				extractedCommunityIDs = append(extractedCommunityIDs, parsedID)
			}
		}
	}

	// 3. Hydratation via MGET sur la collection SpeedCommunity
	multiGetResult, errMGet := redis.SpeedCommunity.GetMany(ctx, extractedCommunityIDs)
	if errMGet != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'hydrater les communautés depuis le cache.",
			errMGet,
		)
	}

	var hydratedCommunitiesList []lite_models.CommunityLiteRequest

	// 4. On boucle sur l'array originel pour conserver l'ordre alphabétique exact renvoyé par l'index
	for _, requestedID := range extractedCommunityIDs {
		if binaryData, isFound := multiGetResult.Found[requestedID]; isFound {
			var communityLitePayload lite_models.CommunityLiteRequest
			if errUnmarshal := msgpack.Unmarshal(binaryData, &communityLitePayload); errUnmarshal == nil {
				hydratedCommunitiesList = append(hydratedCommunitiesList, communityLitePayload)
			} else {
				numan_log.Warn(ctx).Err(errUnmarshal).Int64("community_id", requestedID).Msg("Échec de désérialisation MessagePack d'une communauté")
			}
		}
	}

	return hydratedCommunitiesList, nil
}
