package message_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR D'UN MESSAGE TEXTE (ÉDITION)
// ############################################################################

// UpdateMessage gère la modification d'un message texte existant.
func UpdateMessage(ctx context.Context, callerID int64, input message_models.UpdateMessageInput) (message_models.UpdateMessageOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST (CASCADE L1->L2->L3) ──────────

	messagePayload, errSecurity := security_service.LeftMessage(ctx, input.MessageID, callerID)
	if errSecurity != nil {
		return message_models.UpdateMessageOutput{}, errSecurity
	}

	// ── ÉTAPE 2 : RÈGLES MÉTIER STRICTES ────────────────────────────────────

	if messagePayload.MessageType != variables.MessageTypeText {
		return message_models.UpdateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Seul un message de type texte classique peut être modifié.", nil)
	}

	cleanedContent := pkg.CleanStr(input.Content)
	if cleanedContent == "" {
		return message_models.UpdateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le message ne peut pas être vide.", nil)
	}

	// ── ÉTAPE 3 : APPLICATION DES MODIFICATIONS ─────────────────────────────

	messagePayload.Content = cleanedContent
	messagePayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 4 : MISE À JOUR IMMÉDIATE L1 (OBJECT CACHE) ───────────────────

	_ = object_cache_service.SetMessageInObjectCache(ctx, messagePayload)

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	// PartitionKey = msg.ConversationID pour garantir l'ordre chronologique des opérations sur cette conversation
	errQueue := redis.EnqueueDB(ctx, messagePayload.ID, messagePayload.ConversationID, redis.EntityMessage, redis.ActionUpdate, messagePayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("msg_id", messagePayload.ID).Msg("Échec du Write-Behind pour UpdateMessage")
		return message_models.UpdateMessageOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : DIFFUSION TEMPS RÉEL (ASYNCHRONE) ─────────────────────────

	go func() {
		backgroundCtx := context.Background()

		errBroadcast := realtime_service.BroadcastToConversation(backgroundCtx, messagePayload.ConversationID, "message.updated", messagePayload)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Échec de la diffusion WebSocket pour message.updated")
		}

		// SYNC LEDGER (Trigger granulaire)
		participantIDs := getParticipantIDsForLedgerSync(backgroundCtx, messagePayload.ConversationID)
		_ = cache_service.RecordMessageMutation(backgroundCtx, messagePayload.ConversationID, messagePayload.ID, participantIDs)
	}()

	// ── ÉTAPE 7 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return message_models.UpdateMessageOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
