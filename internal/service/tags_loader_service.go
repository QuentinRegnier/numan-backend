package service

import (
	"context"
	"strings"
	"unicode"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
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
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
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

// StemHashtag extrait la racine morphologique du mot (Stemming).
// Utilisé pour rapprocher des mots comme "marcheur" et "marche".
func StemHashtag(normalizedHashtag string) string {
	stemmedWord, err := snowball.Stem(normalizedHashtag, "french", true)
	if err != nil {
		// En cas d'erreur du dictionnaire, on fallback gracieusement sur le mot normalisé
		return normalizedHashtag
	}
	return stemmedWord
}

// ############################################################################
// # GESTION COMMUNAUTAIRE DES TAGS
// ############################################################################

// GetTagFromKeyword cherche le slug officiel d'un hashtag, et le crée s'il est inconnu.
func GetTagFromKeyword(ctx context.Context, userInput string) (string, bool) {
	cleanInput := NormalizeHashtag(ctx, userInput)
	if cleanInput == "" {
		return "", false
	}

	// 1. Vérification dans le dictionnaire Redis des fautes de frappe (L1)
	canonicalSlug, err := redis.HashtagCanon.HGet(ctx, "map", cleanInput).Result()
	if err == nil && canonicalSlug != "" {
		return canonicalSlug, true
	}

	// 2. Inconnu au bataillon : Il devient son propre tag (Nouveau Tag Communautaire)
	// On l'ajoute silencieusement au SET des tags actifs pour que le Worker de nuit (Cron) l'analyse.
	err = redis.Tags.SAdd(ctx, "active", cleanInput)
	if err != nil {
		numan_log.Error(ctx).Err(err).Str("input", cleanInput).Msg("GetTagFromKeyword: Échec de l'ajout du tag actif")
		return "", false
	}

	return cleanInput, true
}
