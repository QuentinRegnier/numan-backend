package object_cache_service

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (CONVERSATIONS LFU)
// ############################################################################

// GetConversationFromObjectCache récupère le payload complet d'une conversation depuis le cache LFU.
func GetConversationFromObjectCache(ctx context.Context, conversationID int64) (conversation_models.ConversationPayload, error) {
	var conversationPayload conversation_models.ConversationPayload

	errRedis := redis.Conversations.GetObject(ctx, conversationID, &conversationPayload)
	if errRedis != nil {
		return conversation_models.ConversationPayload{}, errRedis // Laisse remonter si c'est un Cache Miss (géré par la cascade)
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
