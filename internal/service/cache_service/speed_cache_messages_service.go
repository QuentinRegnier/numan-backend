package cache_service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : SPEED CACHE (MESSAGES ET TIMELINE)
// ############################################################################

// GetMessageIDsFromSpeedCache résout l'index en L1 (ZSET), en appliquant
// le Plafond Temporel (frozenID) pour gérer la pagination bidirectionnelle.
func GetMessageIDsFromSpeedCache(ctx context.Context, conversationID int64, offsetMessageID int64, paginationLimit int64, scrollDirection string, frozenMessageID int64) ([]int64, error) {
	var idStringsList []string
	var errRedis error

	// ── ÉTAPE 1 : TENTATIVE L1 (ZSET) ───────────────────────────────────────

	if scrollDirection == "top" {
		// Chargement de l'historique (les plus anciens)
		maxScoreThreshold := "+inf"

		// RÈGLE DE GEL : Si un plafond existe, c'est notre maximum absolu
		if frozenMessageID > 0 {
			maxScoreThreshold = strconv.FormatInt(frozenMessageID, 10)
		}

		// GESTION DU SCROLL : L'offset remplace le plafond SEULEMENT s'il est plus petit
		if offsetMessageID > 0 {
			if frozenMessageID > 0 && offsetMessageID > frozenMessageID {
				// Anti-triche : L'utilisateur scrolle depuis un ID du futur, on le cap au gel !
				maxScoreThreshold = strconv.FormatInt(frozenMessageID, 10)
			} else {
				// Parenthèse ouverte = borne exclusive dans la syntaxe Redis
				maxScoreThreshold = fmt.Sprintf("(%d", offsetMessageID)
			}
		}

		idStringsList, errRedis = redis.MessagesIndex.ZRevRangeByScore(ctx, conversationID, maxScoreThreshold, "-inf", paginationLimit)

	} else {
		// Nouveaux messages (les plus récents)
		minScoreThreshold := "-inf"
		if offsetMessageID > 0 {
			minScoreThreshold = fmt.Sprintf("(%d", offsetMessageID) // Exclusif
		}

		maxScoreThreshold := "+inf"
		if frozenMessageID > 0 {
			maxScoreThreshold = strconv.FormatInt(frozenMessageID, 10) // Ne doit jamais dépasser le gel
		}

		idStringsList, errRedis = redis.MessagesIndex.ZRangeByScore(ctx, conversationID, minScoreThreshold, maxScoreThreshold, paginationLimit)
	}

	// ── ÉTAPE 2 : VÉRIFICATION DU CACHE HOLE ET FALLBACK L3 ─────────────────

	if errRedis != nil || len(idStringsList) < int(paginationLimit) {

		if errRedis != nil {
			numan_log.Warn(ctx).Err(errRedis).Msg("Erreur L1 lors de la lecture de l'index des messages")
		}

		// FALLBACK ABSOLU L3 (Cold Storage)
		postgresIDsList, errPg := postgres.FuncLoadMessageIDsPaginated(ctx, conversationID, offsetMessageID, paginationLimit, scrollDirection, frozenMessageID)
		if errPg != nil {
			// Error already wrapped by FuncLoadMessageIDsPaginated; propagate it without wrapping again.
			return nil, errPg
		}

		// RÉHYDRATATION DE L'INDEX L1 (Auto-guérison massive et asynchrone)
		if len(postgresIDsList) > 0 {
			go func(repairedIDs []int64) {
				backgroundCtx := context.Background()
				for _, repairedID := range repairedIDs {
					if err := redis.MessagesIndex.ZAdd(backgroundCtx, conversationID, float64(repairedID), strconv.FormatInt(repairedID, 10)); err != nil {
						numan_log.Warn(backgroundCtx).Err(err).Entity("conversation", conversationID).Action(numan_log.ActionHydrate).Msg("Échec de la réhydratation ZAdd de l'index des messages")
					}
				}
				if err := redis.MessagesIndex.RefreshTTL(backgroundCtx, conversationID); err != nil {
					numan_log.Warn(backgroundCtx).Err(err).Entity("conversation", conversationID).Msg("Échec du rafraîchissement du TTL de l'index des messages")
				}
			}(postgresIDsList)
		}

		return postgresIDsList, nil
	}

	// ── ÉTAPE 3 : PARSING DES IDs TROUVÉS EN L1 ─────────────────────────────

	finalParsedIDs := pkg.ParseInt64List(idStringsList)

	return finalParsedIDs, nil
}
