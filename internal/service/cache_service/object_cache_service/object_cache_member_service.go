package object_cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (MEMBRES LFU)
// ############################################################################

// GetMemberFromObjectCache récupère le payload complet d'un membre depuis le cache LFU.
func GetMemberFromObjectCache(ctx context.Context, conversationID int64, userID int64) (member_models.MemberPayload, error) {
	var memberPayload member_models.MemberPayload

	// Utilisation d'une clé composite encapsulée
	compositeMemberKey := fmt.Sprintf("%d:%d", conversationID, userID)

	errRedis := redis.Members.GetObject(ctx, compositeMemberKey, &memberPayload)
	if errRedis != nil {
		if errors.Is(errRedis, goredis.Nil) {
			return member_models.MemberPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Membre introuvable dans le cache.", errRedis)
		}
		return member_models.MemberPayload{}, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de récupérer le membre depuis l'Object Cache.", errRedis)
	}

	return memberPayload, nil
}

// SetMemberInObjectCache insère ou met à jour un membre dans le cache LFU.
func SetMemberInObjectCache(ctx context.Context, memberPayload member_models.MemberPayload) error {
	compositeMemberKey := fmt.Sprintf("%d:%d", memberPayload.ConversationID, memberPayload.UserID)

	errRedis := redis.Members.SetObject(ctx, compositeMemberKey, memberPayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le membre dans l'Object Cache.",
			errRedis,
		)
	}

	return nil
}
