package ws_handlers

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
)

// HandleReadReceipt gère l'événement de lecture d'une conversation.
func HandleReadReceipt(ctx context.Context, callerID int64, rawPayload []byte) (any, error) {
	var input conversation_models.ReadReceiptInput
	if err := json.Unmarshal(rawPayload, &input); err != nil {
		return nil, numan_error.NewBadRequest("INVALID_PAYLOAD", "Payload invalide.", err)
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	output, err := conversation_service.MarkConversationAsRead(ctx, callerID, input.ConversationID)
	if err != nil {
		return nil, err
	}

	// ICI on retourne la structure complète au lieu du map codé en dur !
	return output, nil
}
