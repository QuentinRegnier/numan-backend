package variables

import (
	"fmt"
	"strings"
)

// ############################################################################
// # CONSTANTES ET RÉFÉRENTIEL LINGUISTIQUE ("UN MAGASIN PAR LANGUE")
// ############################################################################

// Codes entiers compacts pour PostgreSQL (SMALLINT), MongoDB, Redis et les payloads Go.
// Note : 0 correspond au français ("fr"), langue historique et par défaut de la plateforme.
// -1 correspond à une langue legacy/inconnue non encore résolue (sans conversion silencieuse vers l'anglais).
const (
	LangLegacyUnknown = -1 // Publication legacy dont la langue n'est pas encore déterminée
	LangFR            = 0  // Français ("fr") — Langue par défaut de la plateforme
	LangEN            = 1  // Anglais ("en")
	LangES            = 2  // Espagnol ("es")
	LangIT            = 3  // Italien ("it")
	LangPT            = 4  // Portugais ("pt")
	LangDE            = 5  // Allemand ("de")
	LangNL            = 6  // Néerlandais ("nl")
	LangRU            = 7  // Russe ("ru")
	LangAR            = 8  // Arabe ("ar")

	DefaultContentLanguage = LangFR
	DefaultLanguageISO     = "fr"
	LegacyUnknownISO       = "un"
)

// SupportedLanguageCodes liste l'ensemble des codes de langues supportés en production.
var SupportedLanguageCodes = []int{
	LangFR,
	LangEN,
	LangES,
	LangIT,
	LangPT,
	LangDE,
	LangNL,
	LangRU,
	LangAR,
}

var langCodeToISO = map[int]string{
	LangLegacyUnknown: LegacyUnknownISO,
	LangFR:            "fr",
	LangEN:            "en",
	LangES:            "es",
	LangIT:            "it",
	LangPT:            "pt",
	LangDE:            "de",
	LangNL:            "nl",
	LangRU:            "ru",
	LangAR:            "ar",
}

var langISOToCode = map[string]int{
	"un":         LangLegacyUnknown,
	"unknown":    LangLegacyUnknown,
	"fr":         LangFR,
	"fra":        LangFR,
	"french":     LangFR,
	"en":         LangEN,
	"eng":        LangEN,
	"english":    LangEN,
	"es":         LangES,
	"spa":        LangES,
	"spanish":    LangES,
	"it":         LangIT,
	"ita":        LangIT,
	"italian":    LangIT,
	"pt":         LangPT,
	"por":        LangPT,
	"portuguese": LangPT,
	"de":         LangDE,
	"deu":        LangDE,
	"ger":        LangDE,
	"german":     LangDE,
	"nl":         LangNL,
	"nld":        LangNL,
	"dut":        LangNL,
	"dutch":      LangNL,
	"ru":         LangRU,
	"rus":        LangRU,
	"russian":    LangRU,
	"ar":         LangAR,
	"ara":        LangAR,
	"arabic":     LangAR,
}

// langCodeToSnowball associe chaque code de langue à l'algorithme Snowball supporté par github.com/kljensen/snowball.
// Si une langue n'a pas de stemmer Snowball dédié (ex: arabe, néerlandais), la valeur est vide (pas de racinisation destructrice).
var langCodeToSnowball = map[int]string{
	LangFR: "french",
	LangEN: "english",
	LangES: "spanish",
	LangIT: "italian",
	LangPT: "portuguese",
	LangDE: "german",
	LangRU: "russian",
}

// IsSupportedLanguage vérifie si un code entier correspond à une langue active supportée.
func IsSupportedLanguage(code int) bool {
	switch code {
	case LangFR, LangEN, LangES, LangIT, LangPT, LangDE, LangNL, LangRU, LangAR:
		return true
	default:
		return false
	}
}

// IsValidPostLanguage vérifie si le code langue d'un post est valide (langue supportée ou legacy -1).
func IsValidPostLanguage(code int) bool {
	return code == LangLegacyUnknown || IsSupportedLanguage(code)
}

// IsValidContentLanguage est un alias de IsSupportedLanguage pour la validation de la langue active des contenus.
func IsValidContentLanguage(code int) bool {
	return IsSupportedLanguage(code)
}

// LangToISO convertit un code entier de langue en son code ISO 639-1 (ex: 0 -> "fr", 1 -> "en", -1 -> "un").
// Utilisé pour préfixer les clés Redis du MOST Cache ("Un Magasin par Langue").
func LangToISO(code int) string {
	if iso, ok := langCodeToISO[code]; ok {
		return iso
	}
	return DefaultLanguageISO
}

// ParseLanguageISO convertit une chaîne ISO 639-1/639-2 ou nom de langue en code entier.
// Retourne une erreur si la langue n'est pas reconnue.
func ParseLanguageISO(iso string) (int, error) {
	cleaned := strings.ToLower(strings.TrimSpace(iso))
	if cleaned == "" {
		return DefaultContentLanguage, fmt.Errorf("empty language code")
	}
	// Gestion des locales composées (ex: "fr-FR", "en_US")
	if idx := strings.IndexAny(cleaned, "-_"); idx > 0 {
		cleaned = cleaned[:idx]
	}
	if code, ok := langISOToCode[cleaned]; ok {
		return code, nil
	}
	return DefaultContentLanguage, fmt.Errorf("unsupported language code: %s", iso)
}

// SnowballLanguageForCode retourne le nom du stemmer Snowball correspondant au code de langue,
// ou "" si aucun stemmer Snowball n'est applicable.
func SnowballLanguageForCode(code int) string {
	if sb, ok := langCodeToSnowball[code]; ok {
		return sb
	}
	if code == LangLegacyUnknown {
		return "french"
	}
	return ""
}

// ResolveContentLanguage résout de manière déterministe la langue d'une publication à la création :
// 1. Si explicitLang != nil : valide et utilise la langue fournie par le client.
// 2. Sinon, si explicitISO != "" : parse, valide et utilise le code ISO fourni par le client.
// 3. Sinon, utilise userSettingsLang si supporté.
// 4. Sinon, repli déterministe sur DefaultContentLanguage (LangFR = 0).
func ResolveContentLanguage(explicitLang *int, explicitISO string, userSettingsLang int) (int, error) {
	if explicitLang != nil {
		if !IsSupportedLanguage(*explicitLang) {
			return DefaultContentLanguage, fmt.Errorf("unsupported language code: %d", *explicitLang)
		}
		return *explicitLang, nil
	}
	if strings.TrimSpace(explicitISO) != "" {
		code, err := ParseLanguageISO(explicitISO)
		if err != nil || !IsSupportedLanguage(code) {
			return DefaultContentLanguage, fmt.Errorf("unsupported language ISO: %s", explicitISO)
		}
		return code, nil
	}
	if IsSupportedLanguage(userSettingsLang) {
		return userSettingsLang, nil
	}
	return DefaultContentLanguage, nil
}

// GetLanguageFallbackChain retourne la séquence ordonnée de repli linguistique pour le Cold Start
// sans jamais convertir silencieusement des données inconnues, mais en permettant au Magasinier
// de compléter un panier déficitaire de manière déterministe et explicite.
func GetLanguageFallbackChain(primaryLang int) []int {
	if !IsSupportedLanguage(primaryLang) {
		primaryLang = DefaultContentLanguage
	}
	chain := []int{primaryLang}
	// Repli secondaire déterministe : français (langue principale de la plateforme) puis anglais
	if primaryLang != LangFR {
		chain = append(chain, LangFR)
	}
	if primaryLang != LangEN {
		chain = append(chain, LangEN)
	}
	return chain
}

// SupportedLanguageISOs retourne la liste canonique des codes ISO 639-1 supportés
// par la plateforme (dérivée de SupportedLanguageCodes).
func SupportedLanguageISOs() []string {
	isos := make([]string, 0, len(SupportedLanguageCodes))
	for _, code := range SupportedLanguageCodes {
		isos = append(isos, LangToISO(code))
	}
	return isos
}

// NormalizeCommunityLang valide et normalise un code langue ISO 639-1 contre la whitelist
// centrale de la plateforme. Rejette les chaînes vides, "un", les locales non canoniques
// et toute langue non supportée.
func NormalizeCommunityLang(lang string) (string, bool) {
	cleaned := strings.ToLower(strings.TrimSpace(lang))
	if cleaned == "" || len(cleaned) > 8 || cleaned == LegacyUnknownISO {
		return "", false
	}
	for _, code := range SupportedLanguageCodes {
		if LangToISO(code) == cleaned {
			return cleaned, true
		}
	}
	return "", false
}

// IsSupportedCommunityLang retourne true si la chaîne correspond à un code ISO supporté
// pour les communautés publiques.
func IsSupportedCommunityLang(lang string) bool {
	_, ok := NormalizeCommunityLang(lang)
	return ok
}
