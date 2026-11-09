package conversation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : ÉPINGLER / DÉPINDLER UNE CONVERSATION
// ############################################################################

// TogglePinConversation épingle une conversation au sommet de la boîte de réception
// en attribuant un "slot" d'index (0, 1 ou 2) avec un plafond à 3. L'action est idempotente.
func TogglePinConversation(ctx context.Context, callerID int64, input conversation_models.PinConversationInput) (conversation_models.PinConversationOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ────────────────────────────────
	memberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return conversation_models.PinConversationOutput{}, errSecurity
	}

	// Idempotence : si la conversation est déjà épinglée, on retourne un succès silencieux
	if memberPayload.Settings.Pinned >= 0 {
		return conversation_models.PinConversationOutput{}, nil
	}

	// ── ÉTAPE 2 : CALCUL DES SLOTS DISPONIBLES (CASCADE L2 -> L3) ───────────
	var pinnedIndices []int

	pinnedIndices, errMongo := mongo.MongoGetPinnedIndices(callerID)
	if errMongo != nil || len(pinnedIndices) == 0 {
		var errPg error
		pinnedIndices, errPg = postgres.FuncGetPinnedIndices(ctx, callerID)
		if errPg != nil {
			return conversation_models.PinConversationOutput{}, numan_error.NewInternal()
		}
	}

	// Règle métier stricte : 3 épingles maximum
	if len(pinnedIndices) >= variables.MaxPinConversation {
		return conversation_models.PinConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Vous ne pouvez épingler que 3 conversations au maximum.", nil)
	}

	// Détermination du slot vide (0, 1 ou 2)
	usedSlotsMap := make(map[int]bool)
	for _, indexValue := range pinnedIndices {
		usedSlotsMap[indexValue] = true
	}

	availableSlotIndex := -1
	for slotIterator := 0; slotIterator < variables.MaxPinConversation; slotIterator++ {
		if !usedSlotsMap[slotIterator] {
			availableSlotIndex = slotIterator
			break
		}
	}

	memberPayload.Settings.Pinned = availableSlotIndex
	memberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 3 : MISE À JOUR SYNCHRONE L1 (OBJECT ET SPEED CACHE) ──────────
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

	// ── ÉTAPE 4 : PERSISTANCE ASYNCHRONE ────────────────────────────────────
	errQueue := redis.EnqueueDB(ctx, memberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, memberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("member_id", memberPayload.ID).Msg("Échec du Write-Behind pour TogglePinConversation")
		return conversation_models.PinConversationOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return conversation_models.PinConversationOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
