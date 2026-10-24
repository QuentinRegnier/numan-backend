package object_cache_service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (MESSAGES LFU & PIPELINE D'HYDRATATION)
// ############################################################################

// GetMessageFromObjectCache récupère le payload complet d'un message depuis le cache LFU.
func GetMessageFromObjectCache(ctx context.Context, messageID int64) (message_models.MessagePayload, error) {
	var messagePayload message_models.MessagePayload

	errRedis := redis.Messages.GetObject(ctx, messageID, &messagePayload)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return message_models.MessagePayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Message introuvable dans le cache.", errRedis)
		}
		return message_models.MessagePayload{}, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de récupérer le message depuis l'Object Cache.", errRedis)
	}

	return messagePayload, nil
}

// SetMessageInObjectCache insère ou met à jour un message dans le cache LFU.
func SetMessageInObjectCache(ctx context.Context, messagePayload message_models.MessagePayload) error {
	errRedis := redis.Messages.SetObject(ctx, messagePayload.ID, messagePayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'enregistrer le message dans l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// DeleteMessageFromObjectCache supprime un message du cache LFU.
func DeleteMessageFromObjectCache(ctx context.Context, messageID int64) error {
	errRedis := redis.Messages.DeleteObject(ctx, messageID)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer le message de l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// GetMessagesView est le Pipeline d'Hydratation Optimisé pour les Messages (L1 -> L2 -> L3).
func GetMessagesView(ctx context.Context, targetMessageIDs []int64) ([]message_models.MessagePayload, error) {
	if len(targetMessageIDs) == 0 {
		return []message_models.MessagePayload{}, nil
	}

	finalHydratedMessagesList := make([]message_models.MessagePayload, 0, len(targetMessageIDs))
	temporaryMessagesMap := make(map[int64]message_models.MessagePayload)

	// ── ÉTAPE 1 : NIVEAU 1 (REDIS MGET ULTRA-RAPIDE) ────────────────────────
	mgetResult, errMGet := redis.Messages.GetMany(ctx, targetMessageIDs)
	if errMGet != nil {
		numan_log.Warn(ctx).Err(errMGet).Msg("Erreur Redis MGET lors de la récupération des messages (fallback L2 déclenché)")
		mgetResult = &redis.GetManyResult{MissingIDs: targetMessageIDs}
	} else {
		for messageID, binaryData := range mgetResult.Found {
			var messagePayload message_models.MessagePayload
			if errDecode := msgpack.Unmarshal(binaryData, &messagePayload); errDecode == nil {
				temporaryMessagesMap[messageID] = messagePayload
			} else {
				numan_log.Warn(ctx).Err(errDecode).Entity("message", messageID).Msg("Échec du décodage MessagePack d'un message en L1")
				mgetResult.MissingIDs = append(mgetResult.MissingIDs, messageID)
			}
		}
	}

	// ── ÉTAPE 2 : NIVEAU 2 (MONGO FALLBACK WARM STORAGE) ───────────────────
	var stillMissingMessageIDs []int64
	if len(mgetResult.MissingIDs) > 0 {
		mongoMessagesList, errMongo := mongo.MongoLoadMessagesByIDs(ctx, mgetResult.MissingIDs)
		if errMongo == nil {
			mongoFoundMap := make(map[int64]bool)
			for _, mongoMessage := range mongoMessagesList {
				temporaryMessagesMap[mongoMessage.ID] = mongoMessage
				mongoFoundMap[mongoMessage.ID] = true

				// PROMOTION L2 -> L1 (Immédiat en RAM dans une goroutine dédiée)
				go func(msgToPromote message_models.MessagePayload) {
					backgroundCtx := context.Background()
					if err := SetMessageInObjectCache(backgroundCtx, msgToPromote); err != nil {
						numan_log.Warn(backgroundCtx).Err(err).Entity("message", msgToPromote.ID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L2->L1 du message")
					}
				}(mongoMessage)
			}

			for _, missingID := range mgetResult.MissingIDs {
				if !mongoFoundMap[missingID] {
					stillMissingMessageIDs = append(stillMissingMessageIDs, missingID)
				}
			}
		} else {
			numan_log.Warn(ctx).Err(errMongo).Msg("Échec L2 lors de la récupération des messages par IDs")
			stillMissingMessageIDs = mgetResult.MissingIDs
		}
	}

	// ── ÉTAPE 3 : NIVEAU 3 (POSTGRESQL FALLBACK COLD STORAGE) ──────────────
	if len(stillMissingMessageIDs) > 0 {
		postgresMessagesList, errPg := postgres.FuncLoadMessagesByIDs(ctx, stillMissingMessageIDs)
		if errPg == nil {
			for _, pgMessage := range postgresMessagesList {
				temporaryMessagesMap[pgMessage.ID] = pgMessage

				// PROMOTION L3 -> L2 & L1
				go func(msgToPromote message_models.MessagePayload) {
					backgroundCtx := context.Background()

					// L1 : Hydratation immédiate en RAM
					if err := SetMessageInObjectCache(backgroundCtx, msgToPromote); err != nil {
						numan_log.Warn(backgroundCtx).Err(err).Entity("message", msgToPromote.ID).Action(numan_log.ActionHydrate).Msg("Échec de la promotion L3->L1 du message")
					}

					// L2 : Hydratation asynchrone via la queue pour bénéficier du BulkWrite MongoDB des workers
					if err := redis.EnqueueDB(backgroundCtx, msgToPromote.ID, msgToPromote.ConversationID, redis.EntityMessage, redis.ActionUpdate, msgToPromote, redis.TargetMongo); err != nil {
						numan_log.Warn(backgroundCtx).Err(err).Entity("message", msgToPromote.ID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L3->L2 du message")
					}
				}(pgMessage)
			}
		} else {
			numan_log.Error(ctx).Err(errPg).Msg("Échec critique L3 lors du chargement des messages manquants")
		}
	}

	// ── ÉTAPE 4 : ASSEMBLAGE FINAL STRICT ──────────────────────────────────
	for _, targetID := range targetMessageIDs {
		if messagePayload, isExists := temporaryMessagesMap[targetID]; isExists && messagePayload.Visibility {
			finalHydratedMessagesList = append(finalHydratedMessagesList, messagePayload)
		}
	}

	return finalHydratedMessagesList, nil
}
