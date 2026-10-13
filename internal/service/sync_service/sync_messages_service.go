package sync_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : SYNCHRONISATION DES MESSAGES MUTÉS
// ############################################################################

// SyncMessages récupère les payloads frais et hydratés des messages ayant muté
// depuis sinceMs (Création, Édition, Soft Delete).
func SyncMessages(ctx context.Context, callerID int64, input sync_models.SyncMessagesInput) (sync_models.SyncMessagesOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST (CASCADE) ─────────────────────

	_, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return sync_models.SyncMessagesOutput{}, errSecurity
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DES ID MUTÉS (O(log N) RAM L1) ───────────────

	modifiedMessageIDs, errCache := cache_service.GetModifiedMessageIDs(ctx, input.ConversationID, input.SinceMs)
	if errCache != nil {
		numan_log.Error(ctx).Err(errCache).Int64("conv_id", input.ConversationID).Msg("Échec L1 lors de la récupération des deltas de messages")
		return sync_models.SyncMessagesOutput{}, numan_error.NewInternal()
	}

	if len(modifiedMessageIDs) == 0 {
		return sync_models.SyncMessagesOutput{
			Messages: make([]message_models.MessageView, 0),
		}, nil
	}

	// ── ÉTAPE 3 : RÉCUPÉRATION MASSIVE DES MESSAGES (L1 -> L3) ──────────────

	// On récupère le type de la conversation pour le formatage des avatars Twitch.
	conversationPayload, _ := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)

	var hydratedMessageViews []message_models.MessageView
	var missingMessageIDsFromL1 []int64
	temporaryMessagesMap := make(map[int64]message_models.MessagePayload)

	// A. Lecture directe de l'Object Cache (L1)
	// /!\ On n'utilise pas object_cache_service.GetMessagesView car ce dernier filtre les messages
	// avec "visibility = false". Le client a BESOIN des messages supprimés pour purger son SQLite local.
	for _, messageID := range modifiedMessageIDs {
		messagePayload, errL1 := object_cache_service.GetMessageFromObjectCache(ctx, messageID)
		if errL1 == nil && messagePayload.ID != 0 {
			temporaryMessagesMap[messagePayload.ID] = messagePayload
		} else {
			missingMessageIDsFromL1 = append(missingMessageIDsFromL1, messageID)
		}
	}

	// B. Fallback L3 (PostgreSQL) pour les Cache Misses éventuels (Bypass Mongo pour la sûreté des données mutables)
	if len(missingMessageIDsFromL1) > 0 {
		pgMessagesList, errPg := postgres.FuncLoadMessagesByIDs(ctx, missingMessageIDsFromL1)

		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Msg("Échec L3 lors de la réhydratation des messages pour le Delta Sync")
		} else {
			for _, pgMessage := range pgMessagesList {
				temporaryMessagesMap[pgMessage.ID] = pgMessage

				// Auto-guérison L1 silencieuse
				go func(payload message_models.MessagePayload) {
					backgroundCtx := context.Background()
					_ = object_cache_service.SetMessageInObjectCache(backgroundCtx, payload)
				}(pgMessage)
			}
		}
	}

	// ── ÉTAPE 4 : ASSEMBLAGE ET HYDRATATION DES VUES (DTO) ──────────────────

	for _, messageID := range modifiedMessageIDs {
		messagePayload, isMessageResolved := temporaryMessagesMap[messageID]
		if !isMessageResolved {
			continue
		}

		// A. Hydratation de l'image rattachée avec signature HMAC (S3/MinIO)
		if messagePayload.Attachments != nil {
			if rawMediaID, hasMedia := messagePayload.Attachments["media_id"]; hasMedia {
				var targetMediaID int64

				switch parsedValue := rawMediaID.(type) {
				case float64:
					targetMediaID = int64(parsedValue)
				case int64:
					targetMediaID = parsedValue
				}

				if targetMediaID > 0 {
					if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, targetMediaID, messagePayload.SenderID, messagePayload.ID, callerID); errMedia == nil {
						messagePayload.Attachments["media_view"] = mediaView
					}
				}
			}
		}

		// A.bis Hydratation du widget (Widget Message)
		if messagePayload.MessageType == variables.MessageTypeWidget {
			message_service.HydrateWidgetMedia(ctx, &messagePayload, callerID)
		}

		// B. Hydratation de l'expéditeur (Profil Lite L1)
		var resolvedSenderUsername string
		var resolvedSenderAvatar media_models.MediaView
		var resolvedSenderAvatarCommunityID int64

		if senderUserLite, errLite := cache_service.GetUserLite(ctx, messagePayload.SenderID); errLite == nil {
			resolvedSenderUsername = senderUserLite.Username

			if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
				resolvedSenderAvatarCommunityID = senderUserLite.ProfilePictureID // Mode Twitch pour les salons
			} else if senderUserLite.ProfilePictureID > 0 {
				// contextID = 0 (Avatar)
				if avatarView, errAvatar := media_service.GenerateMediaViewCascade(ctx, senderUserLite.ProfilePictureID, messagePayload.SenderID, 0, callerID); errAvatar == nil {
					resolvedSenderAvatar = avatarView
				}
			}
		}

		// C. Hydratation des réactions (Fast Path L1)
		reactionCountsMap, _ := cache_service.GetMessageReactionCounts(ctx, messagePayload.ID)
		userSpecificReaction, _ := cache_service.GetUserReaction(ctx, messagePayload.ID, callerID)

		// D. Composition du DTO
		hydratedMessageViews = append(hydratedMessageViews, message_models.MessageView{
			MessagePayload:          messagePayload,
			SenderUsername:          resolvedSenderUsername,
			SenderAvatar:            resolvedSenderAvatar,
			SenderAvatarCommunityID: resolvedSenderAvatarCommunityID,
			ReactionCounts:          reactionCountsMap,
			UserReaction:            userSpecificReaction,
		})
	}

	if hydratedMessageViews == nil {
		hydratedMessageViews = make([]message_models.MessageView, 0)
	}

	return sync_models.SyncMessagesOutput{
		Messages: hydratedMessageViews,
	}, nil
}
