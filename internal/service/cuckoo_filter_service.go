package service

import (
	"context"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/cuckoo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # MÉMOIRE DU FEED (Cuckoo Filter Distribué via RedisBloom)
// ############################################################################

// HasSeen vérifie dans le Cuckoo Filter Redis (L1) si l'utilisateur a déjà vu ce post.
func HasSeen(ctx context.Context, userID int64, postID int64) bool {
	hasBeenSeen, err := redis.CuckooSeen.CFExists(ctx, userID, postID)

	if err != nil {
		// FALLBACK TRANSPARENT : Si RedisBloom est indisponible ou la clé a disparu,
		// on retourne 'false' pour accepter le post et éviter un crash de l'application.
		// L'utilisateur risque de voir un doublon, mais l'expérience continue.
		return false
	}

	return hasBeenSeen
}

// MarkAsSeen insère le post dans le Cuckoo Filter Redis de l'utilisateur.
func MarkAsSeen(ctx context.Context, userID int64, postID int64) {
	err := redis.CuckooSeen.CFAdd(ctx, userID, postID)
	if err != nil {
		numan_log.Warn(ctx).
			Err(err).
			Int64("user_id", userID).
			Int64("post_id", postID).
			Msg("Impossible d'ajouter le post au Cuckoo Filter")
		return
	}

	// Extension du TTL (Le filtre reste en vie tant que l'utilisateur scrolle activement)
	err = redis.CuckooSeen.RefreshTTL(ctx, userID)
	if err != nil {
		numan_log.Warn(ctx).
			Err(err).
			Int64("user_id", userID).
			Msg("Impossible de prolonger le TTL du Cuckoo Filter")
		return
	}
}

// ResetCuckooFilter détruit le filtre RedisBloom de l'utilisateur de la RAM (L1).
// Utilisé pour l'amnésie algorithmique lors d'un Pull-To-Refresh destructif (/force).
func ResetCuckooFilter(ctx context.Context, userID int64) {
	err := redis.CuckooSeen.DeleteObject(ctx, userID)
	if err != nil {
		numan_log.Warn(ctx).
			Err(err).
			Int64("user_id", userID).
			Msg("Impossible de supprimer le Cuckoo Filter")
		return
	}
}

// ############################################################################
// # VÉRIFICATION GLOBALE D'UNICITÉ (Cascade L1 -> L2 -> L3)
// ############################################################################

// IsUnique vérifie l'unicité d'une valeur (ex: Email, Pseudo).
// Retourne 1 si la donnée est absolument unique, 0 si elle existe déjà.
func IsUnique(ctx context.Context, entityType redis.EntityType, fieldName string, valueToCheck string) int {

	// ── ÉTAPE 1 : Cuckoo Filter RAM Universel (Très Rapide) ───────────────
	mightExist, isFilterActive := cuckoo.MightExist(entityType, fieldName, valueToCheck)
	if isFilterActive && !mightExist {
		return 1 // Sûr à 100% que c'est unique. Zéro appel BDD requis !
	}

	// ── ÉTAPE 2 : Speed Cache L1 (Index Lexicographique ZSET) ──────────────
	// Sécurité absolue en mémoire vive pour les Pseudos avant inscription.
	if entityType == redis.EntityUser && fieldName == "username" {
		lowercaseValue := strings.ToLower(valueToCheck)

		lexResults, err := redis.UsersLex.ZRangeByLex(ctx, "lex", lowercaseValue, 0, 1)
		if err == nil && len(lexResults) > 0 {
			// Le format stocké est "username:id"
			if strings.Split(lexResults[0], ":")[0] == lowercaseValue {
				return 0 // Refusé : Le pseudo existe déjà en RAM
			}
		}
	}

	// ── ÉTAPE 3 : Warm Storage L2 (MongoDB) ────────────────────────────────
	existsInMongo, errMongo := mongo.MongoCheckUnique(ctx, entityType, fieldName, valueToCheck)
	if errMongo == nil && existsInMongo {
		// AUTO-GUÉRISON L1 : Le Cuckoo Filter RAM l'avait oublié (éviction LFU), on le répare.
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, fieldName, valueToCheck)
		return 0
	}

	// ── ÉTAPE 4 : Cold Storage L3 (PostgreSQL - Source de Vérité) ──────────
	existsInPostgres, errPg := postgres.FuncCheckUnique(ctx, entityType, fieldName, valueToCheck)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Str("entity", string(entityType)).Str("field", fieldName).Msg("Échec critique de la validation d'unicité L3")
		return 0 // SÉCURITÉ (Fail Closed) : Dans le doute (ex: Timeout SQL), on refuse l'inscription.
	} else if existsInPostgres {
		// AUTO-GUÉRISON L1 : On répare le Cuckoo Filter
		cuckoo.BroadcastCuckooUpdate(cuckoo.ActionAdd, fieldName, valueToCheck)
		return 0
	}

	// La donnée a survécu à tous les barrages, elle est unique.
	return 1
}

// ############################################################################
// # AMORÇAGE ET WARM-UP (COLD START)
// ############################################################################

// WarmUpCuckooFilter initialise le filtre en mémoire RAM et le charge avec les données critiques de Postgres.
func WarmUpCuckooFilter(ctx context.Context) {
	cuckoo.InitCuckooFilter()
	numan_log.Info(ctx).Msg("Chargement massif des données Postgres vers le Cuckoo Filter L1...")

	// Requête L3 : Récupère uniquement les Pseudos, Emails et Téléphones
	identifiersList, err := postgres.FuncLoadAllUserIdentifiers(ctx)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Msg("Erreur critique lors de l'amorçage du Cuckoo Filter depuis PostgreSQL")
	}

	injectionCount := 0
	for _, identifiers := range identifiersList {
		if identifiers.Username != nil && *identifiers.Username != "" {
			cuckoo.GlobalCuckoo.Insert([]byte("username:" + *identifiers.Username))
		}
		if identifiers.Email != nil && *identifiers.Email != "" {
			cuckoo.GlobalCuckoo.Insert([]byte("email:" + *identifiers.Email))
		}
		if identifiers.Phone != nil && *identifiers.Phone != "" {
			cuckoo.GlobalCuckoo.Insert([]byte("phone:" + *identifiers.Phone))
		}
		injectionCount++
	}

	numan_log.Info(ctx).Int("users_loaded", injectionCount).Msg("Cuckoo Filter amorcé avec succès.")
}
