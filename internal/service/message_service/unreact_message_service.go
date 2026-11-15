package message_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
)

// ############################################################################
// # SERVICE : RETRAIT D'UNE RÉACTION (UNREACT)
// ############################################################################

// UnreactToMessage gère le retrait ciblé d'une réaction sur un message via le Hash Cache.
func UnreactToMessage(ctx context.Context, callerID int64, input message_models.UnreactMessageInput) error {
	if callerID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS AU MESSAGE (ZERO-TRUST) ──────────────────

	messagePayload, errSecurityMsg := security_service.LeftMessage(ctx, input.MessageID, callerID)
	if errSecurityMsg != nil {
		return errSecurityMsg
	}

	callerMemberPayload, errSecurityMem := security_service.LeftMember(ctx, messagePayload.ConversationID, callerID)
	if errSecurityMem != nil || callerMemberPayload.Role < variables.MemberRoleNormal {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous ne faites pas partie de cette conversation.", errSecurityMem)
	}

	// ── ÉTAPE 2 : VÉRIFICATION DE PRÉSENCE (IDEMPOTENCE L1) ─────────────────

	previousEmoji, _ := cache_service.GetUserReaction(ctx, messagePayload.ID, callerID)
	if previousEmoji == "" {
		return nil // L'utilisateur n'avait pas réagi, ou la réaction a déjà été retirée
	}

	// ── ÉTAPE 3 : MISE À JOUR DES COMPTEURS (RAM L1 & WORKER BATCH) ─────────

	_ = cache_service.IncrementReactionCount(ctx, messagePayload.ID, previousEmoji, -1)
	worker.RegisterMessageReaction(messagePayload.ID, previousEmoji, -1)

	_ = cache_service.DeleteUserReaction(ctx, messagePayload.ID, callerID)

	// ── ÉTAPE 4 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	reactionPayload := message_models.MessageReactionPayload{
		MessageID: messagePayload.ID,
		UserID:    callerID,
		// Le Worker n'a besoin que de ces clés primaires pour exécuter l'action DELETE
	}

	errQueue := redis.EnqueueDB(ctx, messagePayload.ID, messagePayload.ConversationID, redis.EntityMessageReaction, redis.ActionDelete, reactionPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("message_id", messagePayload.ID).Msg("Échec du Write-Behind pour UnreactToMessage")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : DIFFUSION TEMPS RÉEL (WEBSOCKETS) ─────────────────────────

	go func() {
		backgroundCtx := context.Background()
		reactionCountsMap, _ := cache_service.GetMessageReactionCounts(backgroundCtx, messagePayload.ID)

		messageViewDto := message_models.MessageView{
			MessagePayload: messagePayload,
			ReactionCounts: reactionCountsMap,
		}

		errBroadcast := realtime_service.BroadcastToConversation(backgroundCtx, messagePayload.ConversationID, "message.unreacted", messageViewDto)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Échec de la diffusion WebSocket pour message.unreacted")
		}

		// SYNC LEDGER : Trigger granulaire pour la base SQLite des clients
		participantIDs := getParticipantIDsForLedgerSync(backgroundCtx, messagePayload.ConversationID)
		_ = cache_service.RecordMessageMutation(backgroundCtx, messagePayload.ConversationID, messagePayload.ID, participantIDs)
	}()

	return nil
}
