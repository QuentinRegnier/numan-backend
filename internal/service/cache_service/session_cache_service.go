package cache_service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : GESTION DU CACHE DES SESSIONS (AUTHENTIFICATION)
// ############################################################################

// Helper local pour protéger le système d'authentification des blocages
// si Redis subit une latence extrême.
func getShortCtx(parentCtx context.Context) (context.Context, context.CancelFunc) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return context.WithTimeout(parentCtx, 2*time.Second)
}

// SetSessionInCache sauvegarde le payload de session et son index de recherche Firebase.
func SetSessionInCache(ctx context.Context, sessionPayload auth_models.SessionsPayload) error {
	timeoutCtx, cancel := getShortCtx(ctx)
	defer cancel()

	errRedisSet := redis.Sessions.SetObject(timeoutCtx, sessionPayload.ID, sessionPayload)
	if errRedisSet != nil {
		numan_log.Error(ctx).Err(errRedisSet).Int64("session_id", sessionPayload.ID).Msg("Impossible de sauvegarder l'objet Session en RAM L1")
		return numan_error.NewInternal()
	}

	if sessionPayload.UserID != 0 && sessionPayload.FirebaseInstallationID != "" {
		// Le préfixe "session_cache:" est géré par la Collection
		indexCompositeKey := fmt.Sprintf("%d:%s", sessionPayload.UserID, sessionPayload.FirebaseInstallationID)

		errRedisIndex := redis.SessionIndexes.SetPrimitive(timeoutCtx, indexCompositeKey, sessionPayload.ID)
		if errRedisIndex != nil {
			numan_log.Warn(timeoutCtx).Err(errRedisIndex).Msg("Échec de la création de l'index de recherche de session Firebase")
		}
	}

	return nil
}

// LoadSessionFromCache charge une session depuis le cache L1 à partir de l'identité de l'appareil.
func LoadSessionFromCache(ctx context.Context, userID int64, firebaseInstallationID string, masterTokenString string) (auth_models.SessionsPayload, error) {
	timeoutCtx, cancel := getShortCtx(ctx)
	defer cancel()

	var resolvedSessionID int64

	if userID != -1 && firebaseInstallationID != "" {
		indexCompositeKey := fmt.Sprintf("%d:%s", userID, firebaseInstallationID)
		retrievedID, errRedisIndex := redis.SessionIndexes.GetInt64(timeoutCtx, indexCompositeKey)

		if errRedisIndex == nil {
			resolvedSessionID = retrievedID
		}
	}

	if resolvedSessionID == 0 {
		return auth_models.SessionsPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Session invalide, introuvable ou expirée en RAM.", nil)
	}

	var sessionPayload auth_models.SessionsPayload
	errRedisGet := redis.Sessions.GetObject(timeoutCtx, resolvedSessionID, &sessionPayload)
	if errRedisGet != nil {
		numan_log.Error(ctx).Err(errRedisGet).Int64("session_id", resolvedSessionID).Msg("Erreur d'infrastructure lors du chargement de l'objet Session")
		return auth_models.SessionsPayload{}, numan_error.NewInternal()
	}

	if masterTokenString != "" && sessionPayload.MasterToken != masterTokenString {
		return auth_models.SessionsPayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Jeton maître d'authentification invalide.", nil)
	}

	return sessionPayload, nil
}

// DeleteSessionFromCache supprime la session, son index, et pose un verrou
// de révocation (Tombstone) dans le cache L1 pour empêcher les attaques par rejeu.
func DeleteSessionFromCache(ctx context.Context, sessionID int64, userID int64, firebaseInstallationID string) error {
	timeoutCtx, cancel := getShortCtx(ctx)
	defer cancel()

	if userID != 0 && firebaseInstallationID != "" {
		indexCompositeKey := fmt.Sprintf("%d:%s", userID, firebaseInstallationID)

		// 1. POSE DU TOMBSTONE VIA L'ABSTRACTION DDD (Blocage immédiat)
		err := redis.SessionBlacklist.SetPrimitive(timeoutCtx, indexCompositeKey, "1")
		if err != nil {
			numan_log.Warn(ctx).Err(err).Int64("session_id", sessionID).Msg("Impossible de poser le Tombstone de révocation de session")
			return err
		}

		// 2. Suppression de l'index de recherche inversé
		err = redis.SessionIndexes.DeletePrimitive(timeoutCtx, indexCompositeKey)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Int64("session_id", sessionID).Msg("Impossible de supprimer l'index de recherche inversé")
			return err
		}
	}

	// 3. Destruction physique de l'objet JSON contenant les tokens
	errRedisDel := redis.Sessions.DeleteObject(timeoutCtx, sessionID)
	if errRedisDel != nil {
		numan_log.Error(ctx).Err(errRedisDel).Int64("session_id", sessionID).Msg("Impossible de supprimer physiquement l'objet Session du cache L1")
		return numan_error.NewInternal()
	}

	return nil
}

// GetFirebaseInstallationIDsCascade récupère tous les tokens FCM des appareils
// de l'utilisateur avec un mécanisme de guérison L1 -> L2 -> L3.
func GetFirebaseInstallationIDsCascade(ctx context.Context, userID int64) ([]string, error) {
	timeoutCtx, cancel := getShortCtx(ctx)
	defer cancel()

	// ── ÉTAPE 1 : TENTATIVE L1 (REDIS VIA SMEMBERS SUR L'ABSTRACTION) ───────

	firebaseTokensList, errRedis := redis.SessionIndexes.SMembers(timeoutCtx, userID)
	if errRedis == nil && len(firebaseTokensList) > 0 {
		return firebaseTokensList, nil
	}

	// ── ÉTAPE 2 : FALLBACK L2 (MONGODB WARM STORAGE) ────────────────────────

	firebaseTokensList, errMongo := mongo.MongoGetFirebaseInstallationIDs(ctx, userID)
	if errMongo == nil && len(firebaseTokensList) > 0 {
		// AUTO-GUÉRISON L1 : On répare le cache RAM
		go healSessionIndexL1(userID, firebaseTokensList)
		return firebaseTokensList, nil
	}

	// ── ÉTAPE 3 : FALLBACK L3 COMPLET (POSTGRESQL COLD STORAGE) ─────────────

	sessionsListFromPg, errPg := postgres.FuncLoadAllUserSessions(ctx, userID)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Int64("user_id", userID).Msg("Erreur L3 lors de la récupération des sessions de l'utilisateur")
		return nil, numan_error.NewInternal()
	}

	if len(sessionsListFromPg) > 0 {
		var extractedFirebaseTokens []string

		for _, sessionRecord := range sessionsListFromPg {
			extractedFirebaseTokens = append(extractedFirebaseTokens, sessionRecord.FirebaseInstallationID)

			// PROMOTION L3 -> L2 & L1 (Auto-guérison massive)
			go func(s auth_models.SessionsPayload) {
				backgroundCtx := context.Background()

				// L1 : Hydratation en RAM de l'objet complet
				err := SetSessionInCache(backgroundCtx, s)
				if err != nil {
					numan_log.Warn(backgroundCtx).Err(err).Int64("session_id", s.ID).Msg("Impossible de poser la session en L1")
					return
				}

				// L2 : Asynchrone vers le Warm Storage Mongo
				err = redis.EnqueueDB(backgroundCtx, s.ID, s.UserID, redis.EntitySession, redis.ActionUpdate, s, redis.TargetMongo)
				if err != nil {
					numan_log.Warn(backgroundCtx).Err(err).Int64("session_id", s.ID).Msg("Impossible de mettre en file d'attente la promotion L2 vers Mongo")
				}
			}(sessionRecord)
		}

		// AUTO-GUÉRISON L1 : On répare l'index de recherche (Set des Firebase IDs)
		go healSessionIndexL1(userID, extractedFirebaseTokens)

		return extractedFirebaseTokens, nil
	}

	return nil, numan_error.NewNotFound(numan_error.CodeNotFound, "Aucun appareil actif configuré pour cet utilisateur.", nil)
}

// healSessionIndexL1 est un helper local asynchrone pour insérer un lot
// d'identifiants Firebase dans le Set d'indexation L1.
func healSessionIndexL1(userID int64, firebaseTokensList []string) {
	backgroundCtx := context.Background()

	// Conversion en slice de "any" requise par l'interface d'abstraction SAdd
	argsForRedis := make([]any, len(firebaseTokensList))
	for index, token := range firebaseTokensList {
		argsForRedis[index] = token
	}

	// Utilisation propre de l'abstraction DDD
	errAdd := redis.SessionIndexes.SAdd(backgroundCtx, userID, argsForRedis...)
	if errAdd != nil {
		numan_log.Warn(backgroundCtx).Err(errAdd).Int64("user_id", userID).Msg("Échec de l'auto-guérison du SessionIndex en L1")
	}

	err := redis.SessionIndexes.RefreshTTL(backgroundCtx, userID)
	if err != nil {
		numan_log.Warn(backgroundCtx).Err(err).Int64("user_id", userID).Msg("Échec de l'auto-guérison du SessionIndex en L1")
		return
	}
}
