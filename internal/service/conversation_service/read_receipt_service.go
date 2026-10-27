package conversation_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : ACCUSÉ DE LECTURE (WATERMARK ET REMISE À ZÉRO)
// ############################################################################

// MarkConversationAsRead remet à zéro le compteur de messages non-lus,
// avance le curseur de lecture d'un participant et émet l'événement WebSocket.
func MarkConversationAsRead(ctx context.Context, callerID int64, conversationID int64) (conversation_models.ReadReceiptOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ────────────────────────────────
	memberPayload, errSecurity := security_service.LeftMember(ctx, conversationID, callerID)
	if errSecurity != nil {
		return conversation_models.ReadReceiptOutput{}, errSecurity
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION L1 DU DERNIER MESSAGE ─────────────────────────
	conversationPayload, errConv := object_cache_service.GetConversationFromObjectCache(ctx, conversationID)
	if errConv != nil || conversationPayload.ID == 0 {
		return conversation_models.ReadReceiptOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable.", nil)
	}

	hasChangedSomething := false

	// A. Remise à zéro des messages non lus
	if memberPayload.UnreadCount > 0 {
		memberPayload.UnreadCount = 0
		hasChangedSomething = true
	}

	// B. Avancement du curseur de lecture (Watermark)
	// Règle métier : On avance toujours, on ne recule jamais.
	if conversationPayload.LastMessageID > memberPayload.LastReadMessageID {
		memberPayload.LastReadMessageID = conversationPayload.LastMessageID
		hasChangedSomething = true

		// ENREGISTREMENT INSTANTANÉ DANS LE HASH REDIS L1 (O(1))
		_ = cache_service.SetWatermarkInSpeedCache(ctx, conversationID, callerID, conversationPayload.LastMessageID)
	}

	// ── ÉTAPE 3 : SAUVEGARDE ET PERSISTANCE (Seulement en cas de mutation) ──
	if hasChangedSomething {
		memberPayload.UpdatedAt = domain.NowMillis()

		_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)
		_ = cache_service.ResetMemberUnreadCountInSpeedCache(ctx, conversationID, callerID, conversationPayload.LastMessageID)

		errQueue := redis.EnqueueDB(ctx, memberPayload.ID, conversationID, redis.EntityMembers, redis.ActionUpdate, memberPayload, redis.TargetAll)
		if errQueue != nil {
			numan_log.Error(ctx).Err(errQueue).Int64("user_id", callerID).Msg("Échec du Write-Behind pour la remise à zéro des non-lus")
			// Pas de retour d'erreur HTTP pour ne pas bloquer l'UX de l'utilisateur
		}
	}

	// ── ÉTAPE 4 : BROADCAST ET VIDEUR (CAPPING DE GROUPE) ───────────────────
	userSettingsPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, callerID)

	// Autorisation de vie privée : "SendReadReceipts"
	if errSettings == nil && userSettingsPayload.Privacy.SendReadReceipts {

		// LE VIDEUR (CAPPING) : Vérification O(1) de la taille du groupe
		activeParticipantCount, _ := redis.ConvParticipants.SCard(ctx, conversationID)

		// Règle d'infrastructure : on bloque le broadcast si > 50 membres
		// pour prévenir la saturation réseau (Tempête d'acquittements / ACK Storm).
		if activeParticipantCount <= int64(variables.MaxActiveParticipantsForBroadcast) {
			websocketPayload := map[string]any{
				"conversation_id":      conversationID,
				"user_id":              callerID,
				"last_read_message_id": memberPayload.LastReadMessageID,
			}

			_ = realtime_service.BroadcastToConversation(ctx, conversationID, "conversation.read", websocketPayload)
		}
	}

	// ── ÉTAPE 5 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────
	// On marque l'activité même sans changement, car ce signal prouve la présence active
	// de l'utilisateur sur l'écran de messagerie, ce qui repousse le mode "Dormant" algorithmique.
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return conversation_models.ReadReceiptOutput{
		InboxUpdateAt: domain.TimeToMillis(time.UnixMilli(latestActivityTimestampMs)),
	}, nil
}
