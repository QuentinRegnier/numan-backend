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
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
)

// ############################################################################
// # SERVICE : AJOUTER OU MODIFIER UNE RÉACTION (EMOJI)
// ############################################################################

// ReactToMessage gère l'ajout ou la modification d'une réaction sur un message via le Hash Cache L1.
func ReactToMessage(ctx context.Context, callerID int64, input message_models.ReactMessageInput) error {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS AU MESSAGE (ZERO-TRUST) ──────────────────

	messagePayload, errSecurityMsg := security_service.LeftMessage(ctx, input.MessageID, callerID)
	if errSecurityMsg != nil {
		return errSecurityMsg
	}

	callerMemberPayload, errSecurityMem := security_service.LeftMember(ctx, messagePayload.ConversationID, callerID)
	if errSecurityMem != nil || callerMemberPayload.Role < variables.MemberRoleNormal {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous ne faites pas partie de cette conversation.", errSecurityMem)
	}

	sanitizedEmoji := pkg.CleanStr(input.Reaction)

	// ── ÉTAPE 2 : VÉRIFICATION IDEMPOTENCE (RAM L1) ─────────────────────────

	previousEmoji, _ := cache_service.GetUserReaction(ctx, messagePayload.ID, callerID)
	if previousEmoji == sanitizedEmoji {
		return nil // Idempotence : L'utilisateur a cliqué sur le même emoji, succès silencieux.
	}

	// ── ÉTAPE 3 : AUTORISATION DES NOUVEAUX TYPES DE RÉACTIONS ──────────────
	if !messagePayload.AllowNewReactions {
		reactionCountsMap, errReaction := cache_service.GetMessageReactionCounts(ctx, messagePayload.ID)
		if errReaction != nil {
			return errReaction
		}
		count, exists := reactionCountsMap[sanitizedEmoji]
		if !exists || count <= 0 {
			return numan_error.NewForbidden(numan_error.CodeForbidden, "You cannot add new types of reactions to this specific message.", nil)
		}
	}

	// ── ÉTAPE 4 : APPLICATION DU DELTA (RAM L1 & WORKER BATCH) ──────────────

	// Si l'utilisateur avait une ancienne réaction, on la décrémente d'abord.
	if previousEmoji != "" {
		_ = cache_service.IncrementReactionCount(ctx, messagePayload.ID, previousEmoji, -1)
		worker.RegisterMessageReaction(messagePayload.ID, previousEmoji, -1)
	}

	// Incrémentation de la nouvelle réaction
	_ = cache_service.IncrementReactionCount(ctx, messagePayload.ID, sanitizedEmoji, 1)
	worker.RegisterMessageReaction(messagePayload.ID, sanitizedEmoji, 1)

	// Sauvegarde de l'état personnel de l'utilisateur
	_ = cache_service.SetUserReaction(ctx, messagePayload.ID, callerID, sanitizedEmoji)

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	reactionPayload := message_models.MessageReactionPayload{
		ID:        pkg.GenerateID(),
		MessageID: messagePayload.ID,
		UserID:    callerID,
		Reaction:  sanitizedEmoji,
		CreatedAt: domain.NowMillis(),
	}

	// L'ActionCreate déclenchera un UPSERT côté Worker grâce à la contrainte UNIQUE SQL (message_id, user_id)
	errQueue := redis.EnqueueDB(ctx, reactionPayload.ID, messagePayload.ConversationID, redis.EntityMessageReaction, redis.ActionCreate, reactionPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("message_id", messagePayload.ID).Msg("Échec du Write-Behind pour ReactToMessage")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : DIFFUSION TEMPS RÉEL (WEBSOCKETS) ─────────────────────────

	go func() {
		backgroundCtx := context.Background()
		reactionCountsMap, _ := cache_service.GetMessageReactionCounts(backgroundCtx, messagePayload.ID)

		messageViewDto := message_models.MessageView{
			MessagePayload: messagePayload,
			ReactionCounts: reactionCountsMap,
			// UserReaction n'est pas envoyé en broadcast (spécifique à chaque client)
		}

		errBroadcast := realtime_service.BroadcastToConversation(backgroundCtx, messagePayload.ConversationID, "message.reacted", messageViewDto)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Échec de la diffusion WebSocket pour message.reacted")
		}

		// SYNC LEDGER : Trigger granulaire pour mettre à jour la base SQLite des clients
		participantIDs := getParticipantIDsForLedgerSync(backgroundCtx, messagePayload.ConversationID)
		_ = cache_service.RecordMessageMutation(backgroundCtx, messagePayload.ConversationID, messagePayload.ID, participantIDs)
	}()

	return nil
}
