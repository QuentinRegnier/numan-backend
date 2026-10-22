package security_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
)

// ############################################################################
// # SERVICE DE SÉCURITÉ : DROITS DE PROPRIÉTÉ D'UN MESSAGE
// ############################################################################

// LeftMessage récupère un message complet (L1 -> L2 -> L3) et vérifie
// que l'utilisateur appelant est bien l'auteur du message ciblé.
func LeftMessage(ctx context.Context, messageID int64, userID int64) (message_models.MessagePayload, error) {
	var messagePayload message_models.MessagePayload
	var isMessageFound bool

	// ── ÉTAPE 1 : TENTATIVE L1 (OBJECT CACHE - LFU) ─────────────────────────

	if cachedMessage, err := object_cache_service.GetMessageFromObjectCache(ctx, messageID); err == nil && cachedMessage.ID != 0 {
		messagePayload = cachedMessage
		isMessageFound = true
	} else {

		// ── ÉTAPE 2 : TENTATIVE L2 (MONGODB WARM STORAGE) ───────────────────

		mongoMessagesList, errMongo := mongo.MongoLoadMessagesByIDs(ctx, []int64{messageID})
		if errMongo == nil && len(mongoMessagesList) > 0 {
			messagePayload = mongoMessagesList[0]
			isMessageFound = true

			// AUTO-GUÉRISON L1 (Immédiat en RAM)
			_ = object_cache_service.SetMessageInObjectCache(ctx, messagePayload)

		} else {

			// ── ÉTAPE 3 : FALLBACK ABSOLU L3 (POSTGRESQL COLD STORAGE) ──────

			pgMessagesList, errPg := postgres.FuncLoadMessagesByIDs(ctx, []int64{messageID})
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("message_id", messageID).Msg("Erreur L3 lors de la vérification de sécurité d'un message")
				return message_models.MessagePayload{}, errPg
			}

			if len(pgMessagesList) > 0 {
				messagePayload = pgMessagesList[0]
				isMessageFound = true

				// AUTO-GUÉRISON L1 (Immédiat en RAM)
				_ = object_cache_service.SetMessageInObjectCache(ctx, messagePayload)

				// AUTO-GUÉRISON L2 (Asynchrone via Worker Mongo)
				go func(msg message_models.MessagePayload) {
					backgroundCtx := context.Background()
					// PartitionKey = ConversationID pour grouper les messages chronologiquement
					_ = redis.EnqueueDB(backgroundCtx, msg.ID, msg.ConversationID, redis.EntityMessage, redis.ActionUpdate, msg, redis.TargetMongo)
				}(messagePayload)
			}
		}
	}

	// ── ÉTAPE 4 : VÉRIFICATION DES RÈGLES DE SÉCURITÉ ───────────────────────

	if !isMessageFound || !messagePayload.Visibility {
		// Furtivité absolue : on ne dit pas si le message existe mais est caché (Soft Delete)
		return message_models.MessagePayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Message introuvable.", nil)
	}

	if messagePayload.SenderID != userID {
		return message_models.MessagePayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous n'êtes pas l'auteur de ce message.", nil)
	}

	return messagePayload, nil
}
