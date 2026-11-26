package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/kljensen/snowball"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ############################################################################
// # NORMALISATION ET CANONICALISATION DES HASHTAGS
// ############################################################################

// NormalizeHashtag applique la normalisation lexicale stricte.
// Opérations:
// 1. Suppression des caractères non-alphanumériques en bordure
// 2. Translittération des caractères accentués vers l'ASCII le plus proche
// 3. Conversion en minuscules
func NormalizeHashtag(ctx context.Context, rawHashtag string) string {
	// Étape 1 : Nettoyage des bordures (Trim)
	trimmedHashtag := strings.TrimFunc(rawHashtag, func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
	})

	if trimmedHashtag == "" {
		return ""
	}

	// Étape 2 : Translittération (ex: "é" -> "e", "ç" -> "c")
	transliteratedHashtag := transliterateToASCII(ctx, trimmedHashtag)

	// Étape 3 : Minuscules
	return strings.ToLower(transliteratedHashtag)
}

// transliterateToASCII convertit les caractères accentués vers leurs équivalents ASCII purs.
// Méthode : Décomposition NFD -> Suppression des diacritiques (Catégorie Unicode Mn) -> Recomposition NFC.
func transliterateToASCII(c context.Context, inputString string) string {
	transformerChain := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	cleanString, _, err := transform.String(transformerChain, inputString)
	if err != nil {
		numan_log.Warn(c).Err(err).Str("input", inputString).Msg("transliterateToASCII: Échec de la translittération, retour de l'input brut")
		return inputString
	}
	return cleanString
}

// StemHashtagWithLang extrait la racine morphologique du mot (Stemming) selon la langue du post.
// Utilise le dictionnaire Snowball correspondant (french, english, spanish, etc.) ou fallback sans stemming.
func StemHashtagWithLang(normalizedHashtag string, lang int) string {
	if normalizedHashtag == "" {
		return ""
	}
	snowballLang := variables.SnowballLanguageForCode(lang)
	if snowballLang == "" {
		return normalizedHashtag
	}
	stemmedWord, err := snowball.Stem(normalizedHashtag, snowballLang, true)
	if err != nil || stemmedWord == "" {
		return normalizedHashtag
	}
	return stemmedWord
}

// ############################################################################
// # GESTION COMMUNAUTAIRE DES TAGS & QUARANTAINE COLD-START
// ############################################################################

// ResolveCanonicalTag interroge le dictionnaire de canonicalisation par langue (avec fallback global)
// sans enregistrer automatiquement un tag non vérifié dans les tags actifs.
func ResolveCanonicalTag(ctx context.Context, rawTag string, lang ...int) string {
	cleanInput := NormalizeHashtag(ctx, rawTag)
	if cleanInput == "" {
		return ""
	}
	resolvedLang := variables.DefaultContentLanguage
	if len(lang) > 0 {
		resolvedLang = variables.NormalizeLangForCache(lang[0])
	}

	if redis.HashtagCanon.IsReady() {
		if canonicalSlug, err := redis.HashtagCanon.HGet(ctx, variables.HashtagCanonSubKey(resolvedLang), cleanInput); err == nil && canonicalSlug != "" {
			return canonicalSlug
		}
		if canonicalSlug, err := redis.HashtagCanon.HGet(ctx, "map", cleanInput); err == nil && canonicalSlug != "" {
			return canonicalSlug
		}
	}
	return cleanInput
}

// GetTagFromKeywordWithLang résout le slug canonique d'un hashtag dans une langue donnée.
// Pour contrer la pollution Cold-Start (fautes de frappe uniques ou spam d'un seul auteur),
// lorsqu'un contributorID > 0 est fourni, le nouveau tag passe par une quarantaine 24h
// (seuil: variables.TagQuarantineMinAuthors auteurs distincts) avant d'intégrer tags:active:{lang}.
func GetTagFromKeywordWithLang(ctx context.Context, userInput string, lang int, contributorID int64) (string, bool) {
	cleanInput := NormalizeHashtag(ctx, userInput)
	if cleanInput == "" {
		return "", false
	}
	normLang := variables.NormalizeLangForCache(lang)

	// Les auto-tags auteurs ("user_123") ne doivent jamais polluer le dictionnaire public de hashtags
	if strings.HasPrefix(cleanInput, variables.AuthorTagPrefix) {
		return cleanInput, true
	}

	// 1. Vérification dans le dictionnaire Redis des fautes de frappe par langue (L1)
	if redis.HashtagCanon.IsReady() {
		if canonicalSlug, err := redis.HashtagCanon.HGet(ctx, variables.HashtagCanonSubKey(normLang), cleanInput); err == nil && canonicalSlug != "" {
			return canonicalSlug, true
		}
		if canonicalSlug, err := redis.HashtagCanon.HGet(ctx, "map", cleanInput); err == nil && canonicalSlug != "" {
			return canonicalSlug, true
		}
	}

	// 2. Si le tag est déjà actif dans cette langue, on le retourne immédiatement
	activeSubKey := variables.ActiveTagsSubKey(normLang)
	if redis.Tags.IsReady() {
		if isActive, err := redis.Tags.SIsMember(ctx, activeSubKey, cleanInput); err == nil && isActive {
			return cleanInput, true
		}
	}

	// 3. Quarantaine Cold-Start si contributorID > 0 et seuil > 1
	if contributorID > 0 && variables.TagQuarantineMinAuthors > 1 && redis.TagQuarantine.IsReady() {
		quarantineSubKey := fmt.Sprintf("%d:%s", normLang, cleanInput)
		_ = redis.TagQuarantine.SAdd(ctx, quarantineSubKey, contributorID)
		_ = redis.TagQuarantine.RefreshTTL(ctx, quarantineSubKey)
		distinctAuthors, errCard := redis.TagQuarantine.SCard(ctx, quarantineSubKey)
		if errCard == nil && int(distinctAuthors) < variables.TagQuarantineMinAuthors {
			// Le tag est utilisable sur le post mais reste en quarantaine vis-à-vis de tags:active et de l'autocomplétion globale
			return cleanInput, true
		}
	}

	// 4. Promotion dans les tags actifs de la langue (et clé legacy "active" pour rétrocompatibilité)
	if redis.Tags.IsReady() {
		if err := redis.Tags.SAdd(ctx, activeSubKey, cleanInput); err != nil {
			numan_log.Error(ctx).Err(err).Str("input", cleanInput).Int("lang", normLang).Msg("GetTagFromKeywordWithLang: Échec de l'ajout du tag actif")
			return "", false
		}
		_ = redis.Tags.SAdd(ctx, "active", cleanInput)
	}
	if redis.TagsLex.IsReady() {
		_ = redis.TagsLex.ZAddLex(ctx, variables.ActiveTagsLexSubKey(normLang), cleanInput)
		_ = redis.TagsLex.ZAddLex(ctx, "active", cleanInput)
	}

	return cleanInput, true
}
