package conversation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

// ############################################################################
// # SERVICE : RETRAIT D'ÉPINGLE (UNPIN) D'UNE CONVERSATION
// ############################################################################

// UnpinConversation gère le retrait d'une épingle sur une conversation.
// Libère le "slot" (0, 1 ou 2) pour de futures épingles. Action idempotente.
func UnpinConversation(ctx context.Context, callerID int64, input conversation_models.UnpinConversationInput) (conversation_models.UnpinConversationOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS (CASCADE L1->L2->L3) ─────────────────────

	memberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return conversation_models.UnpinConversationOutput{}, errSecurity
	}

	// ── ÉTAPE 2 : IDEMPOTENCE (VÉRIFICATION D'ÉTAT) ─────────────────────────

	if memberPayload.Settings.Pinned == -1 {
		return conversation_models.UnpinConversationOutput{}, nil // Déjà retirée, on sort proprement
	}

	// ── ÉTAPE 3 : APPLICATION DU RETRAIT ────────────────────────────────────

	memberPayload.Settings.Pinned = -1
	memberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE EN RAM L1 ───────────────────────────

	_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)

	memberLiteRequest := lite_models.MemberLiteRequest{
		ConversationID:    memberPayload.ConversationID,
		UserID:            memberPayload.UserID,
		Role:              memberPayload.Role,
		Settings:          service.ToMemberSettingsLite(memberPayload.Settings),
		UnreadCount:       memberPayload.UnreadCount,
		FrozenMessageID:   memberPayload.FrozenMessageID,
		LastReadMessageID: memberPayload.LastReadMessageID,
		JoinedAt:          memberPayload.JoinedAt,
	}
	_ = cache_service.UpdateMemberSpeedCache(ctx, memberLiteRequest)

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	// La clé de partition est l'ID de la conversation pour conserver l'ordre des requêtes
	errQueue := redis.EnqueueDB(ctx, memberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, memberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("member_id", memberPayload.ID).Msg("Échec de mise en file asynchrone pour le retrait d'épingle")
		return conversation_models.UnpinConversationOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return conversation_models.UnpinConversationOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
