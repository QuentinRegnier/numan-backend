package ws_handlers

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

type typingPayload struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
}

func (p *typingPayload) Validate() error {
	if p == nil || p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

func HandleTyping(ctx context.Context, callerID int64, rawPayload []byte, isTyping bool) error {
	var input typingPayload
	if err := json.Unmarshal(rawPayload, &input); err != nil {
		return numan_error.NewBadRequest("INVALID_PAYLOAD", "Payload invalide.", err)
	}

	if err := input.Validate(); err != nil {
		return err
	}

	// 1. SÉCURITÉ ZERO-TRUST : L1 Speed Cache
	mem, err := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if err != nil || mem.Role < 0 {
		return numan_error.NewForbidden("ACCESS_DENIED", "Accès refusé.", err)
	}

	// 2. CHOIX DE L'ÉVÉNEMENT
	eventType := "typing.stopped"
	if isTyping {
		eventType = "typing.started"
	}

	// 3. BROADCAST VOLATIL (Directement via Redis Pub/Sub, Zéro BDD)
	broadcastPayload := map[string]any{
		"conversation_id": input.ConversationID,
		"user_id":         callerID,
	}

	return realtime_service.BroadcastToConversation(ctx, input.ConversationID, eventType, broadcastPayload)
}
