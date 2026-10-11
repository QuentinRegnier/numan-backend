package pkg

import (
	"context"
	"fmt"
	"html"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
)

// Ce fichier contient les fonctions utilitaires pures (sans état, helpers)

// ValidateStruct permet de valider manuellement une structure
// en utilisant le moteur de Gin (et les tags `binding`).
func ValidateStruct(obj any) error {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		return v.Struct(obj)
	}
	numan_log.Error(context.Background()).Msg("Impossible de charger le validateur Gin (binding.Validator.Engine)")
	return numan_error.NewInternal()
}

// CleanStr : Nettoyage anti-XSS et suppression des espaces superflus.
// NOTE : La protection contre les injections SQL est déléguée aux requêtes préparées (Postgres/database/sql).
func CleanStr(input string) string {
	cleaned := strings.TrimSpace(input)
	return html.EscapeString(cleaned)
}

// generateToken : Création JWT
func GenerateToken(userID int64, firebaseInstallationID string, expirationSeconds int) (string, error) {
	claims := jwt.MapClaims{
		"sub": fmt.Sprintf("%d", userID),
		"dev": firebaseInstallationID, // Ajout du claim personnalisé
		"exp": time.Now().Add(time.Second * time.Duration(expirationSeconds)).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		numan_log.Error(context.Background()).Msg("Variable d'environnement JWT_SECRET manquante")
		return "", numan_error.NewInternal()
	}
	return token.SignedString([]byte(secret))
}

// ToMap convertit une structure en map[string]any en préservant les types Go exacts (int, time.Time, etc.)
// Cela corrige les erreurs de validation "attendu struct, reçu int64".
func ToMap(in any) (map[string]any, error) {
	out := make(map[string]any)
	v := reflect.ValueOf(in)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		numan_log.Error(context.Background()).Str("kind", v.Kind().String()).Msg("ToMap attend une struct en paramètre")
		return nil, numan_error.NewInternal()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)

		// Récupération de la clé via le tag bson ou json
		tag := field.Tag.Get("bson")
		if tag == "" {
			tag = field.Tag.Get("json")
		}

		// Nettoyage des options comme "name,omitempty"
		key := strings.Split(tag, ",")[0]

		// On ignore les champs sans tag ou ignorés
		if key == "" || key == "-" {
			continue
		}

		out[key] = v.Field(i).Interface()
	}
	return out, nil
}

// toStruct convertit une map[string]any en structure (comme User)
// en respectant les tags `bson:"..."` pour que la validation du schéma fonctionne.
func ToStruct(m map[string]any, out any) error {
	// 1. On convertit la map en bytes BSON
	data, err := bson.Marshal(m)
	if err != nil {
		return err
	}

	// 2. On reconvertit les bytes en struct cible
	err = bson.Unmarshal(data, out)
	if err != nil {
		return err
	}

	return nil
}

// exists vérifie si une valeur existe dans une slice
func Exists[T comparable](slice []T, value T) bool {
	return slices.Contains(slice, value)
}

func SliceUniqueInt64(slice []int64) []int64 {
	keys := make(map[int64]bool)
	var list []int64
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func SliceUniqueStr(slice []string) []string {
	keys := make(map[string]bool)
	var list []string
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

// GetUserIDFromContext extrait de manière sécurisée l'ID utilisateur du contexte Gin.
// Elle gère les conversions de types string (JWT sub), float64 (JSON) et int64.
func GetUserIDFromContext(c *gin.Context) (int64, error) {
	val, exists := c.Get("userID")
	if !exists {
		return 0, numan_error.NewForbidden("UNAUTHORIZED", "Utilisateur non identifié dans le contexte.", nil)
	}

	switch v := val.(type) {
	case int64:
		return v, nil
	case string:
		id, err := ParseInt64Strict(v)
		if err != nil {
			return 0, numan_error.NewBadRequest("INVALID_USER_ID", "Le format de l'ID utilisateur est invalide.", err)
		}
		return id, nil
	case float64:
		return int64(v), nil
	case int:
		return int64(v), nil
	default:
		numan_log.Error(c).Interface("val", val).Msg("Type inattendu pour le userID extrait du contexte")
		return 0, numan_error.NewInternal()
	}
}

// =========================================================================
// UTILITAIRES DE TEXTE ET MENTIONS
// =========================================================================

// Compilation globale de la regex pour des performances optimales (O(N) sans recompilation).
// On cherche le motif @{id}, ex: @{123456}
var mentionRegex = regexp.MustCompile(`@{([0-9]+)}`)

// ExtractMentions parse une chaîne de caractères et retourne une liste
// dédupliquée des IDs d'utilisateurs mentionnés.
func ExtractMentions(content string) []int64 {
	matches := mentionRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	var ids []int64
	for _, match := range matches {
		if len(match) == 2 {
			if id, err := ParseInt64Strict(match[1]); err == nil {
				ids = append(ids, id)
			}
		}
	}

	// Déduplication via l'utilitaire existant dans ce même package
	return SliceUniqueInt64(ids)
}

// ParseInt64List silently converts a slice of strings to a slice of int64.
// Malformed values are ignored. Ideal for parsing Redis ZSET/SET returns.
func ParseInt64List(stringList []string) []int64 {
	if len(stringList) == 0 {
		return nil
	}

	intList := make([]int64, 0, len(stringList))

	for _, str := range stringList {
		if parsed, err := strconv.ParseInt(str, 10, 64); err == nil {
			intList = append(intList, parsed)
		}
	}

	return intList
}

// ParseInt64 is a shortcut to parse a single string to int64, defaulting to 0 on error.
func ParseInt64(str string) int64 {
	parsed, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		numan_log.Warn(context.Background()).Err(err).Str("input", str).Msg("ParseInt64: Impossible de parser l'input en int64, retour par défaut 0")
		return 0
	}
	return parsed
}

// ParseInt64Strict converts a string to int64 and returns the error for strict validation.
// It acts as a unified wrapper for strconv.ParseInt(str, 10, 64).
func ParseInt64Strict(str string) (int64, error) {
	return strconv.ParseInt(str, 10, 64)
}

// genericMessageResponse is a generic JSON response containing a single message.
type genericMessageResponse struct {
	Message string `json:"message"`
}
