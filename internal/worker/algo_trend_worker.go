package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER : HASHTAG TRENDS (ÉVALUATION DES TENDANCES MONDIALES)
// ############################################################################

// startHashtagTrendCron lance l'évaluation des tendances mondiales de hashtags (TDD §3.3).
// Il tourne à intervalle régulier pour maintenir le Top 100 des tags sans saturer le CPU.
func startHashtagTrendCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Moteur de Tendances Hashtags...")

	go func() {
		ticker := time.NewTicker(variables.TrendCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processHashtagTrends(ctx)
			}
		}
	}()
}

// processHashtagTrends calcule le score de popularité des tags basés sur la viralité des posts récents.
func processHashtagTrends(ctx context.Context) {
	// ── ÉTAPE 1 : EXTRACTION DES MEILLEURS POSTS MONDIAUX (L1) ──────────────
	now := time.Now().UTC()
	dateStr := now.Format("20060102")

	// Reconstruction de la clé physique du ZSET Global
	dailyGlobalKey := fmt.Sprintf(variables.RedisKeyTrendGlobalDaily, dateStr)

	// Récupération du Top 1000 de la journée (O(log(N) + M))
	topPosts, err := redis.ZRevRangeWithScores(ctx, dailyGlobalKey, 0, variables.TrendTopPostsLimit)
	if err != nil || len(topPosts) == 0 {
		return // Pas d'activité suffisante pour dégager une tendance
	}

	postScoresByTag := make(map[string]map[int64]float64)
	postAgesByTag := make(map[string]map[int64]float64)

	// ── ÉTAPE 2 : HYDRATATION ET GROUPEMENT O(N) ────────────────────────────
	for _, item := range topPosts {
		var postID int64 // Normalisation robuste du type renvoyé par go-redis
		switch v := item.Member.(type) {
		case string:
			postID = pkg.ParseInt64(v)
		case []byte:
			postID = pkg.ParseInt64(string(v))
		default:
			postID = pkg.ParseInt64(fmt.Sprintf("%v", v))
		}

		if postID == 0 {
			continue
		}

		score := item.Score

		// Hydratation via le Fallback global (L1 -> L2 -> L3)
		p, errHydration := getPostWithFallback(ctx, postID)
		if errHydration != nil || p.Visibility != variables.PostVisibilityAll {
			continue // Sécurité : Seuls les posts publics (PostVisibilityAll) alimentent les tendances mondiales
		}

		ageSeconds := now.Sub(domain.MillisToTime(p.CreatedAt)).Seconds()

		// Fusion des Tags Directs et Indirects pour une couverture sémantique totale
		allTags := append(p.Hashtags, p.IndirectHashtags...)
		for _, tag := range allTags {
			if postScoresByTag[tag] == nil {
				postScoresByTag[tag] = make(map[int64]float64)
				postAgesByTag[tag] = make(map[int64]float64)
			}
			postScoresByTag[tag][postID] = score
			postAgesByTag[tag][postID] = ageSeconds
		}
	}

	// ── ÉTAPE 3 : CALCUL MATHÉMATIQUE ET PERSISTANCE (PIPELINE L1) ──────────
	if len(postScoresByTag) > 0 {
		pipe := redis.Tags.Pipeline()
		trendingKey := redis.Tags.Key("trending")

		for tag, scoresMap := range postScoresByTag {
			agesMap := postAgesByTag[tag]

			// Appel du moteur mathématique pur (Implémentation du TDD §3.3)
			trendScore := service.ComputeHashtagTrendScore(scoresMap, agesMap)

			if trendScore > 0 {
				pipe.Do(ctx, "ZADD", trendingKey, trendScore, tag)
			}
		}

		// Protection RAM stricte (LFU/Cap) : On ne conserve que le Top 100
		pipe.Do(ctx, "ZREMRANGEBYRANK", trendingKey, 0, variables.TrendTagsRetention)

		_, errPipe := pipe.Exec(ctx)
		if errPipe != nil {
			numan_log.Error(ctx).Err(errPipe).Msg("Échec critique lors de l'enregistrement des tendances Hashtags en RAM (L1)")
		} else {
			numan_log.Info(ctx).Msg("Moteur de Tendances : Mise à jour du Top 100 mondial réussie.")
		}
	}
}
