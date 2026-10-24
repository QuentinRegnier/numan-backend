package object_cache_service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (CONVERSATIONS LFU)
// ############################################################################

// GetConversationFromObjectCache récupère le payload complet d'une conversation depuis le cache LFU.
func GetConversationFromObjectCache(ctx context.Context, conversationID int64) (conversation_models.ConversationPayload, error) {
	var conversationPayload conversation_models.ConversationPayload

	errRedis := redis.Conversations.GetObject(ctx, conversationID, &conversationPayload)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return conversation_models.ConversationPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable dans le cache.", errRedis)
		}
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de récupérer la conversation depuis l'Object Cache.", errRedis)
	}

	return conversationPayload, nil
}

// SetConversationInObjectCache insère ou met à jour une conversation dans le cache LFU.
func SetConversationInObjectCache(ctx context.Context, conversationPayload conversation_models.ConversationPayload) error {
	errRedis := redis.Conversations.SetObject(ctx, conversationPayload.ID, conversationPayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder la conversation dans l'Object Cache.",
			errRedis,
		)
	}
	return nil
}
