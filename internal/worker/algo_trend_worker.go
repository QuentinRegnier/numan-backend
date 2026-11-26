package worker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER : HASHTAG TRENDS (ÉVALUATION DES TENDANCES PAR LANGUE)
// ############################################################################

// startHashtagTrendCron lance l'évaluation des tendances de hashtags par langue (TDD §3.3).
// Il tourne à intervalle régulier pour maintenir le Top des tags par langue sans saturer le CPU.
func startHashtagTrendCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Moteur de Tendances Hashtags (Par Langue)...")

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

// processHashtagTrends calcule le score de popularité des tags basés sur la viralité des posts récents par langue.
// Ce worker est le propriétaire exclusif de redis.HashtagLeaderboard (et synchronise redis.Tags "trending" pour rétrocompatibilité).
func processHashtagTrends(ctx context.Context) {
	token, acquired, errLock := redis.AcquireCronLock(ctx, "hashtag_trends", variables.TrendCronInterval/2)
	if errLock == nil && !acquired && token == "" && redis.CronLocks.IsReady() {
		return
	}
	if token != "" {
		defer func() {
			_ = redis.ReleaseCronLock(ctx, "hashtag_trends", token)
		}()
	}

	now := domain.NowTime().UTC()
	dateStr := now.Format("20060102")
	yesterdayStr := now.Add(-24 * time.Hour).Format("20060102")

	for _, lang := range variables.SupportedLanguageCodes {
		processHashtagTrendsForLang(ctx, lang, now, dateStr, yesterdayStr)
	}
}

func processHashtagTrendsForLang(ctx context.Context, lang int, now time.Time, dateStr, yesterdayStr string) {
	if !redis.TrendGlobalDaily.IsReady() {
		return
	}

	// 1. Lecture de la partition journalière de la langue (avec complément hier à minuit et fallback legacy)
	topPosts, err := redis.TrendGlobalDaily.ZRevRangeWithScores(ctx, variables.TrendGlobalDailySubKey(lang, dateStr), 0, variables.TrendTopPostsLimit)
	if (err != nil || len(topPosts) < 10) && yesterdayStr != "" {
		if yPosts, errY := redis.TrendGlobalDaily.ZRevRangeWithScores(ctx, variables.TrendGlobalDailySubKey(lang, yesterdayStr), 0, variables.TrendTopPostsLimit); errY == nil && len(yPosts) > 0 {
			topPosts = append(topPosts, yPosts...)
		}
	}
	if len(topPosts) == 0 && lang == variables.DefaultContentLanguage {
		// Fallback compatibilité ascendante sur la clé sans préfixe langue ou ancienne clé most_cache:trend:global:daily:%s
		if legacyPosts, errLeg := redis.TrendGlobalDaily.ZRevRangeWithScores(ctx, dateStr, 0, variables.TrendTopPostsLimit); errLeg == nil && len(legacyPosts) > 0 {
			topPosts = legacyPosts
		} else if oldKeyPosts, errOld := redis.ZRevRangeWithScores(ctx, fmt.Sprintf(variables.RedisKeyTrendGlobalDaily, dateStr), 0, variables.TrendTopPostsLimit); errOld == nil && len(oldKeyPosts) > 0 {
			topPosts = oldKeyPosts
		}
	}
	if len(topPosts) == 0 {
		return
	}

	postScoresByTag := make(map[string]map[int64]float64)
	postAgesByTag := make(map[string]map[int64]float64)
	seenPosts := make(map[int64]bool, len(topPosts))

	// 2. Hydratation et groupement O(N) (uniquement visibility == 0 et hors auto-tags user_*)
	for _, item := range topPosts {
		var postID int64
		switch v := item.Member.(type) {
		case string:
			postID = pkg.ParseInt64(v)
		case []byte:
			postID = pkg.ParseInt64(string(v))
		default:
			postID = pkg.ParseInt64(fmt.Sprintf("%v", v))
		}
		if postID <= 0 || seenPosts[postID] {
			continue
		}
		seenPosts[postID] = true

		p, errHydration := getPostWithFallback(ctx, postID)
		if errHydration != nil || p.Visibility != variables.PostVisibilityAll {
			continue
		}

		ageSeconds := now.Sub(domain.MillisToTime(p.CreatedAt)).Seconds()
		if ageSeconds < 0 {
			ageSeconds = 0
		}

		// Tags directs (100% du score)
		for _, rawTag := range p.Hashtags {
			tag := service.ResolveCanonicalTag(ctx, rawTag, lang)
			if tag == "" || strings.HasPrefix(tag, variables.AuthorTagPrefix) {
				continue
			}
			if postScoresByTag[tag] == nil {
				postScoresByTag[tag] = make(map[int64]float64)
				postAgesByTag[tag] = make(map[int64]float64)
			}
			postScoresByTag[tag][postID] = item.Score
			postAgesByTag[tag][postID] = ageSeconds
		}

		// Tags indirects issus des commentaires (pondération 0.65)
		for _, rawTag := range p.IndirectHashtags {
			tag := service.ResolveCanonicalTag(ctx, rawTag, lang)
			if tag == "" || strings.HasPrefix(tag, variables.AuthorTagPrefix) {
				continue
			}
			if postScoresByTag[tag] == nil {
				postScoresByTag[tag] = make(map[int64]float64)
				postAgesByTag[tag] = make(map[int64]float64)
			}
			if _, alreadyDirect := postScoresByTag[tag][postID]; !alreadyDirect {
				postScoresByTag[tag][postID] = item.Score * variables.IndirectTagScoreWeight
				postAgesByTag[tag][postID] = ageSeconds
			}
		}
	}

	if len(postScoresByTag) == 0 {
		return
	}

	// 3. Calcul mathématique du score de tendance par tag et plafonnement Top N
	members := make([]redis.ZMember, 0, len(postScoresByTag))
	for tag, scoresMap := range postScoresByTag {
		agesMap := postAgesByTag[tag]
		trendScore := service.ComputeHashtagTrendScore(scoresMap, agesMap)
		if trendScore > 0 {
			members = append(members, redis.ZMember{
				Score:  trendScore,
				Member: tag,
			})
		}
	}
	if len(members) == 0 {
		return
	}

	sort.Slice(members, func(i, j int) bool {
		return members[i].Score > members[j].Score
	})
	maxTags := variables.TDDMaxZSET
	if maxTags <= 0 {
		maxTags = 100
	}
	if len(members) > maxTags {
		members = members[:maxTags]
	}

	// 4. Remplacement atomique dans redis.HashtagLeaderboard par langue (+ "global" et Tags "trending" pour la langue par défaut)
	if redis.HashtagLeaderboard.IsReady() {
		if err := redis.HashtagLeaderboard.ZSetReplaceAtomic(ctx, variables.HashtagLeaderboardSubKey(lang), members, 0); err != nil {
			numan_log.Error(ctx).Err(err).Int("lang", lang).Msg("Échec critique lors de l'enregistrement du leaderboard de hashtags par langue")
			return
		}
		if lang == variables.DefaultContentLanguage {
			_ = redis.HashtagLeaderboard.ZSetReplaceAtomic(ctx, "global", members, 0)
		}
	}
	if lang == variables.DefaultContentLanguage && redis.Tags.IsReady() {
		_ = redis.Tags.ZSetReplaceAtomic(ctx, "trending", members, 0)
	}
}
