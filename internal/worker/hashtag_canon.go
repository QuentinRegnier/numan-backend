package worker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
)

// ############################################################################
// # WORKER : HASHTAG CANON (RÉSOLUTION DES FAUTES DE FRAPPE & ALIASING)
// ############################################################################

// startHashtagCanonCron lance un worker qui calcule les similarités (Levenshtein)
// entre les tags communautaires toutes les 24h pour absorber les fautes de frappe.
func startHashtagCanonCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Canoniseur de Hashtags (Cron 24h)...")

	go func() {
		ticker := time.NewTicker(variables.HashtagCanonCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processHashtagCanonicalization(ctx)
			}
		}
	}()
}

// processHashtagCanonicalization est le cœur de l'algorithme de nettoyage et de fusion sémantique par langue.
func processHashtagCanonicalization(ctx context.Context) {
	token, acquired, errLock := redis.AcquireCronLock(ctx, "hashtag_canon", 10*time.Minute)
	if errLock == nil && !acquired && token == "" && redis.CronLocks != nil && redis.CronLocks.Client != nil {
		return
	}
	if token != "" {
		defer func() {
			_ = redis.ReleaseCronLock(ctx, "hashtag_canon", token)
		}()
	}

	allTagsSet := make(map[string]struct{})
	for _, lang := range variables.SupportedLanguageCodes {
		tags := processHashtagCanonForLang(ctx, lang)
		for _, t := range tags {
			allTagsSet[t] = struct{}{}
		}
	}

	// Traitement également du réservoir legacy "active" sur la langue par défaut
	if redis.Tags != nil && redis.Tags.Client != nil {
		if legacyTags, err := redis.Tags.SMembers(ctx, "active"); err == nil && len(legacyTags) > 0 {
			for _, t := range legacyTags {
				allTagsSet[t] = struct{}{}
			}
			processTagListForCanon(ctx, legacyTags, variables.DefaultContentLanguage)
		}
	}

	if len(allTagsSet) > 0 {
		allTags := make([]string, 0, len(allTagsSet))
		for t := range allTagsSet {
			allTags = append(allTags, t)
		}
		persistCommunityTags(ctx, allTags)
	}
}

func processHashtagCanonForLang(ctx context.Context, lang int) []string {
	if redis.Tags == nil || redis.Tags.Client == nil {
		return nil
	}
	tags, err := redis.Tags.SMembers(ctx, variables.ActiveTagsSubKey(lang))
	if err != nil || len(tags) < 2 {
		return tags
	}
	processTagListForCanon(ctx, tags, lang)
	return tags
}

// selectCanonicalAndTypo choisit le tag canonique entre t1 et t2 :
// 1. Le tag ayant la plus forte popularité (cardinalité dans TagPosts).
// 2. À popularité égale, le mot le plus LONG (complet, non tronqué) l'emporte.
// 3. À longueur égale, ordre lexicographique déterministe.
func selectCanonicalAndTypo(ctx context.Context, t1, t2 string, lang int) (canon string, typo string) {
	var pop1, pop2 int64
	if redis.TagPosts != nil && redis.TagPosts.Client != nil {
		pop1, _ = redis.TagPosts.ZCard(ctx, variables.TagIndexSubKey(lang, t1))
		pop2, _ = redis.TagPosts.ZCard(ctx, variables.TagIndexSubKey(lang, t2))
	}
	if pop1 != pop2 {
		if pop2 > pop1 {
			return t2, t1
		}
		return t1, t2
	}
	if len(t1) != len(t2) {
		if len(t2) > len(t1) {
			return t2, t1
		}
		return t1, t2
	}
	if t2 < t1 {
		return t2, t1
	}
	return t1, t2
}

func processTagListForCanon(ctx context.Context, tags []string, lang int) {
	if len(tags) < 2 {
		return
	}

	// ── ÉTAPE 1 : BLOCKING PAR RACINE MORPHOLOGIQUE (STEMMING PAR LANGUE) ────
	// Évite l'explosion quadratique O(N²) globale en regroupant d'abord les tags
	// qui partagent exactement la même racine Snowball dans la langue cible.
	blocks := make(map[string][]string)
	for _, t := range tags {
		if len(t) < variables.HashtagCanonMinLength || strings.HasPrefix(t, variables.AuthorTagPrefix) {
			continue
		}
		stem := service.StemHashtagWithLang(t, lang)
		if stem == "" {
			continue
		}
		blocks[stem] = append(blocks[stem], t)
	}

	aliasMap := make(map[string]string)

	// ── ÉTAPE 2 : COMPARAISON LEVENSHTEIN INTRA-BLOC ────────────────────────
	for _, group := range blocks {
		if len(group) < 2 {
			continue
		}
		sort.Strings(group)
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				t1 := group[i]
				t2 := group[j]
				if t1 == t2 {
					continue
				}
				distNorm := normalizedLevenshtein(t1, t2)
				if distNorm <= variables.HashtagCanonMaxDistance {
					canon, typo := selectCanonicalAndTypo(ctx, t1, t2, lang)
					aliasMap[typo] = canon
				}
			}
		}
	}

	if len(aliasMap) == 0 {
		return
	}

	// Résolution transitive des chaînes d'alias (A -> B -> C => A -> C)
	for typo, canon := range aliasMap {
		visited := map[string]bool{typo: true}
		curr := canon
		for {
			next, hasNext := aliasMap[curr]
			if !hasNext || visited[next] {
				break
			}
			visited[next] = true
			curr = next
		}
		aliasMap[typo] = curr
	}

	// ── ÉTAPE 3 : MISE À JOUR DU DICTIONNAIRE D'ALIAS & FUSION DES ZSETS ────
	if redis.HashtagCanon != nil && redis.HashtagCanon.Client != nil {
		pipe := redis.HashtagCanon.Pipeline()
		canonSubKey := variables.HashtagCanonSubKey(lang)
		for typo, canon := range aliasMap {
			if typo == canon {
				continue
			}
			pipe.HSet(ctx, redis.HashtagCanon.Key(canonSubKey), typo, canon)
			if lang == variables.DefaultContentLanguage {
				pipe.HSet(ctx, redis.HashtagCanon.Key("map"), typo, canon)
			}
		}
		if _, errExec := pipe.Exec(ctx); errExec != nil {
			numan_log.Error(ctx).Err(errExec).Int("lang", lang).Msg("Échec critique lors de l'enregistrement des alias dans Redis (L1)")
			return
		}
	}

	// Fusion des ZSETs (TagPosts, TrendTagDaily, TrendTagWeekly) de la faute de frappe vers le tag canonique
	now := domain.NowTime().UTC()
	dateStr := now.Format("20060102")
	year, isoWeek := now.ISOWeek()
	weekStr := fmt.Sprintf("%d-W%02d", year, isoWeek)

	for typo, canon := range aliasMap {
		if typo == canon {
			continue
		}
		mergeAliasTagZSETs(ctx, lang, typo, canon, dateStr, weekStr)
	}
}

func mergeAliasTagZSETs(ctx context.Context, lang int, typo, canon, dateStr, weekStr string) {
	if redis.TagPosts != nil && redis.TagPosts.Client != nil {
		canonIdx := variables.TagIndexSubKey(lang, canon)
		typoIdx := variables.TagIndexSubKey(lang, typo)
		_ = redis.TagPosts.ZUnionStore(ctx, canonIdx, []any{canonIdx, typoIdx}, "MAX")
		_ = redis.TagPosts.DeleteObject(ctx, typoIdx)
	}
	if redis.TrendTagDaily != nil && redis.TrendTagDaily.Client != nil {
		canonDaily := variables.TrendTagDailySubKey(lang, canon, dateStr)
		typoDaily := variables.TrendTagDailySubKey(lang, typo, dateStr)
		_ = redis.TrendTagDaily.ZUnionStore(ctx, canonDaily, []any{canonDaily, typoDaily}, "MAX")
		_ = redis.TrendTagDaily.DeleteObject(ctx, typoDaily)
	}
	if redis.TrendTagWeekly != nil && redis.TrendTagWeekly.Client != nil {
		canonWeekly := variables.TrendTagWeeklySubKey(lang, canon, weekStr)
		typoWeekly := variables.TrendTagWeeklySubKey(lang, typo, weekStr)
		_ = redis.TrendTagWeekly.ZUnionStore(ctx, canonWeekly, []any{canonWeekly, typoWeekly}, "MAX")
		_ = redis.TrendTagWeekly.DeleteObject(ctx, typoWeekly)
	}
	if redis.Tags != nil && redis.Tags.Client != nil {
		_ = redis.Tags.SRem(ctx, variables.ActiveTagsSubKey(lang), typo)
	}
}

// ============================================================================
// MOTEUR MATHÉMATIQUE DE DISTANCE D'ÉDITION
// ============================================================================

// normalizedLevenshtein calcule la distance de Levenshtein normalisée.
// TDD §3.3 — Formule: d_Lev(h_i, h_j) = Lev(h_i, h_j) / max(|h_i|, |h_j|)
// Retourne une valeur dans [0.0, 1.0]: 0.0 = chaînes identiques, 1.0 = totalement différentes.
func normalizedLevenshtein(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	maxLen := la
	if lb > maxLen {
		maxLen = lb
	}

	if maxLen == 0 {
		return 0.0
	}

	return float64(levenshteinRunes(ra, rb)) / float64(maxLen)
}

// levenshteinRunes calcule la distance d'édition de Levenshtein entre deux slices de runes.
// Implémentation ultra-optimisée en espace O(min(|a|,|b|)) via deux rangées glissantes.
func levenshteinRunes(a, b []rune) int {
	la, lb := len(a), len(b)

	// Optimisation: garantir |a| ≤ |b| pour minimiser l'allocation mémoire.
	if la > lb {
		a, b = b, a
		la, lb = lb, la
	}

	// Deux rangées de la matrice DP (espace O(la) au lieu de O(la·lb)).
	prev := make([]int, la+1)
	curr := make([]int, la+1)

	// Initialisation: prev[j] = j (coût de suppression des j premiers chars de a).
	for j := 0; j <= la; j++ {
		prev[j] = j
	}

	for i := 1; i <= lb; i++ {
		curr[0] = i
		for j := 1; j <= la; j++ {
			cost := 1
			if b[i-1] == a[j-1] {
				cost = 0
			}
			// min(insertion, suppression, substitution)
			curr[j] = min3Int(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}

	return prev[la]
}

func min3Int(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// persistCommunityTags sauvegarde les tags communautaires dans PostgreSQL.
// Utilise la fonction SQL content.func_upsert_community_tags pour l'insertion de masse idempotente.
func persistCommunityTags(ctx context.Context, tags []string) {
	if len(tags) == 0 || postgres.WriteDB() == nil {
		return
	}

	query := "SELECT content.func_upsert_community_tags($1)"

	var inserted int
	err := postgres.WriteDB().QueryRowContext(ctx, query, pq.Array(tags)).Scan(&inserted)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec lors de la persistance SQL des tags communautaires (L3)")
	} else {
		numan_log.Info(ctx).Int("count", len(tags)).Msg("Persistance SQL des tags communautaires terminée avec succès.")
	}
}
