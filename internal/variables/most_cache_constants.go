package variables

import (
	"fmt"
	"strings"
	"time"
)

// ############################################################################
// # CLÉS REDIS — NOMENCLATURE UNIFIÉE (IMMUABLES)
// ############################################################################

const (
	// Clés legacy conservées uniquement pour fallback en lecture
	RedisKeyTrendGlobalDaily   = "most_cache:trend:global:daily:%s"
	RedisKeyTrendTagDaily      = "most_cache:trend:tag:%s:daily:%s"
	RedisKeyHashtagLeaderboard = "most_cache:trend:hashtag:leaderboard"

	// Classements Stricts (Pour l'interface utilisateur uniquement)
	RedisKeyStrictLikes  = "most_cache:strict:likes"
	RedisKeyStrictViews  = "most_cache:strict:views"
	RedisKeyStrictRecent = "most_cache:strict:recent"

	// Sous-clés normalisées pour redis.RankedPosts ("most_cache:strict")
	StrictTypeLikes  = "likes"
	StrictTypeViews  = "views"
	StrictTypeRecent = "recent"

	// TTLs métier des ZSETs & SETs éphémères du Magasin (MOST Cache)
	TrendGlobalHourlyTTL = 48 * time.Hour
	TrendGlobalDailyTTL  = 72 * time.Hour
	TrendTagDailyTTL     = 72 * time.Hour
	TrendTagWeeklyTTL    = 14 * 24 * time.Hour
	AuthorPostWindowTTL  = 24 * time.Hour
	TagQuarantineTTL     = ShortTTL
	IndirectTagSetTTL    = 7 * 24 * time.Hour
)

// ############################################################################
// # LIMITES OPÉRATIONNELLES ET PARAMÈTRES MATHÉMATIQUES (DYNAMIQUES)
// ############################################################################

var (
	MaxStrictElements    = 5000 // Top absolu pour l'UI (Likes, Vues)
	MaxTagElements       = 5000 // Taille max pour l'historique brut d'un tag
	MaxTrendingTagsInRAM = 1000 // Top 1000 des tags mondiaux (UI)
	MaxPostsPerTagInRAM  = 1000 // Limite stricte pour le Speed Cache par tag

	TDDMaxZSET    = 500  // MAX_ZSET — taille max par ZSET de tendance
	TDDCandidates = 1000 // |C| — taille de l'ensemble de candidats pour le feed_service
	TDDFeedSize   = 50   // K_feed — taille du feed_service personnalisé retourné

	TDDSigmaHours         = 2.0  // σ_h (h) — lissage gaussien sur l'heure
	TDDWeightView         = 0.1  // w_view — Poids d'une vue pure
	TDDPhiReported        = 0.5  // φ_mod — pénalité si le post_service est signalé
	TDDHashtagWindowHours = 48.0 // Fenêtre de validité pour les trends hashtags

	// Pondération et seuils anti-abus (Tags indirects & Quarantaine Cold-Start)
	IndirectTagScoreWeight       = 0.65 // Pondération des hashtags indirects issus des commentaires
	IndirectTagConsensusMinUsers = 3    // Nombre minimum d'utilisateurs distincts requis pour valider un tag indirect
	TagQuarantineMinAuthors      = 3    // Seuil d'auteurs/posts distincts en 24h avant promotion dans tags:active:{lang}
)

// NormalizeLangForCache garantit qu'un code langue (y compris LangLegacyUnknown = -1)
// est résolu vers une partition linguistique valide dans Redis.
func NormalizeLangForCache(lang int) int {
	if IsSupportedLanguage(lang) {
		return lang
	}
	return DefaultContentLanguage
}

// TrendGlobalHourlySubKey génère la sous-clé pour redis.TrendGlobalHourly ("trend:global:hourly:{lang}:{hourStr}").
func TrendGlobalHourlySubKey(lang int, hourStr string) string {
	return fmt.Sprintf("%d:%s", NormalizeLangForCache(lang), hourStr)
}

// TrendGlobalDailySubKey génère la sous-clé pour redis.TrendGlobalDaily ("trend:global:daily:{lang}:{dateStr}").
func TrendGlobalDailySubKey(lang int, dateStr string) string {
	return fmt.Sprintf("%d:%s", NormalizeLangForCache(lang), dateStr)
}

// TrendTagDailySubKey génère la sous-clé pour redis.TrendTagDaily ("trend:tag:daily:{lang}:{slug}:{dateStr}").
func TrendTagDailySubKey(lang int, slug, dateStr string) string {
	return fmt.Sprintf("%d:%s:%s", NormalizeLangForCache(lang), slug, dateStr)
}

// TrendTagWeeklySubKey génère la sous-clé pour redis.TrendTagWeekly ("trend:tag:weekly:{lang}:{slug}:{weekStr}").
func TrendTagWeeklySubKey(lang int, slug, weekStr string) string {
	return fmt.Sprintf("%d:%s:%s", NormalizeLangForCache(lang), slug, weekStr)
}

// HashtagLeaderboardSubKey génère la sous-clé pour redis.HashtagLeaderboard ("hashtag:leaderboard:{lang}").
func HashtagLeaderboardSubKey(lang int) string {
	return fmt.Sprintf("%d", NormalizeLangForCache(lang))
}

// NormalizeStrictRankType normalise les variantes d'identifiants de classement strict ("strict:likes", "most_cache:strict:likes", "likes").
func NormalizeStrictRankType(rankType string) string {
	clean := strings.TrimPrefix(rankType, "most_cache:")
	clean = strings.TrimPrefix(clean, "strict:")
	clean = strings.TrimSuffix(clean, ":strict")
	switch clean {
	case StrictTypeLikes:
		return StrictTypeLikes
	case StrictTypeViews:
		return StrictTypeViews
	default:
		return StrictTypeRecent
	}
}

// StrictRankSubKey génère la sous-clé pour redis.RankedPosts ("most_cache:strict:{type}:{lang}").
func StrictRankSubKey(rankType string, lang int) string {
	return fmt.Sprintf("%s:%d", NormalizeStrictRankType(rankType), NormalizeLangForCache(lang))
}

// TagIndexSubKey génère la sous-clé pour redis.TagPosts ("most_cache:idx:tag:{lang}:{slug}").
func TagIndexSubKey(lang int, slug string) string {
	return fmt.Sprintf("%d:%s", NormalizeLangForCache(lang), slug)
}

// GraphEdgesSubKey génère la sous-clé pour redis.GraphEdges ("graph_cache:tag_edges:{lang}:{tag}").
func GraphEdgesSubKey(lang int, tag string) string {
	return fmt.Sprintf("%d:%s", NormalizeLangForCache(lang), tag)
}

// ActiveTagsSubKey génère la sous-clé pour redis.Tags ("tags:active:{lang}").
func ActiveTagsSubKey(lang int) string {
	return fmt.Sprintf("active:%d", NormalizeLangForCache(lang))
}

// ActiveTagsLexSubKey génère la sous-clé pour redis.TagsLex ("tags:lex:active:{lang}").
func ActiveTagsLexSubKey(lang int) string {
	return fmt.Sprintf("active:%d", NormalizeLangForCache(lang))
}

// HashtagCanonSubKey génère la sous-clé pour redis.HashtagCanon ("hashtag:canon:map:{lang}").
func HashtagCanonSubKey(lang int) string {
	return fmt.Sprintf("map:%d", NormalizeLangForCache(lang))
}
