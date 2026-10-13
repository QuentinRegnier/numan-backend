package message_service

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # UTILITAIRES : RÉCUPÉRATION ET SYNCHRONISATION
// ############################################################################

// getParticipantIDsForLedgerSync récupère les identifiants actifs de la conversation
// en O(1) depuis la RAM pour déclencher le Sync Ledger des clients connectés.
func getParticipantIDsForLedgerSync(ctx context.Context, conversationID int64) []int64 {
	participantsStringList, err := redis.ConvParticipants.SMembers(ctx, conversationID)
	if err != nil || len(participantsStringList) == 0 {
		return nil
	}

	participantIDs := pkg.ParseInt64List(participantsStringList)

	return participantIDs
}

// HydrateWidgetMedia inspecte les attachments d'un message widget et hydrate cover_media_view
// si un cover_media_id valide est présent dans visuals.
func HydrateWidgetMedia(ctx context.Context, messagePayload *message_models.MessagePayload, callerID int64) {
	if messagePayload == nil || messagePayload.MessageType != variables.MessageTypeWidget || messagePayload.Attachments == nil {
		return
	}

	rawWidget, exists := messagePayload.Attachments["widget"]
	if !exists || rawWidget == nil {
		return
	}

	widgetPayload, err := message_models.ParseWidgetPayload(rawWidget)
	if err != nil {
		return
	}

	coverMediaID := widgetPayload.Visuals.CoverMediaID
	if coverMediaID <= 0 {
		return
	}

	mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, coverMediaID, messagePayload.SenderID, messagePayload.ID, callerID)
	if errMedia != nil {
		return
	}

	// Injection sécurisée et non-destructive de cover_media_view
	widgetCopy := make(map[string]any)
	if m, ok := rawWidget.(map[string]any); ok {
		for k, v := range m {
			widgetCopy[k] = v
		}
	} else {
		data, errMarshal := json.Marshal(rawWidget)
		if errMarshal == nil {
			_ = json.Unmarshal(data, &widgetCopy)
		}
	}

	widgetCopy["cover_media_view"] = mediaView

	// Copie superficielle des attachments pour éviter les mutations partagées en mémoire
	newAttachments := make(map[string]any, len(messagePayload.Attachments))
	for k, v := range messagePayload.Attachments {
		newAttachments[k] = v
	}
	newAttachments["widget"] = widgetCopy
	messagePayload.Attachments = newAttachments
}
