package message_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES MESSAGES (HISTORIQUE)
// ############################################################################

// GetMessages récupère l'historique d'une conversation avec une hydratation riche (Avatars, pseudos, réactions).
func GetMessages(ctx context.Context, callerID int64, input message_models.GetMessagesInput) ([]message_models.MessageView, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS (SÉCURITÉ ZERO-TRUST) ────────────────────

	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil || callerMemberPayload.Role < variables.MemberRoleNormal {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous ne faites pas partie de cette conversation.", errSecurity)
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DE LA CONVERSATION (CASCADE L1 -> L2 -> L3) ──

	conversationPayload, errCache := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)

	if errCache != nil || conversationPayload.ID == 0 {
		var errMongo error
		conversationPayload, errMongo = mongo.MongoGetConversation(ctx, input.ConversationID)

		if errMongo != nil || conversationPayload.ID == 0 {
			var errPg error
			conversationPayload, errPg = postgres.FuncGetConversation(ctx, input.ConversationID)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("conv_id", input.ConversationID).Msg("Erreur L3 lors de la récupération de la conversation")
				return nil, numan_error.NewInternal()
			}
			if conversationPayload.ID == 0 {
				return nil, numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable.", nil)
			}

			// AUTO-GUÉRISON L3 -> L2 (Asynchrone via Queue)
			go func(c conversation_models.ConversationPayload) {
				bgCtx := context.Background()
				_ = redis.EnqueueDB(bgCtx, c.ID, c.ID, redis.EntityConversation, redis.ActionUpdate, c, redis.TargetMongo)
			}(conversationPayload)
		}

		// AUTO-GUÉRISON L3/L2 -> L1 (Immédiate en RAM)
		_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)
	}

	mustHideSystemMessages := conversationPayload.Settings.HideSystemMessages

	// ── ÉTAPE 3 : RÉSOLUTION D'INDEX (QUELS MESSAGES CHARGER ?) ─────────────

	messageIDsList, errIndex := cache_service.GetMessageIDsFromSpeedCache(ctx, input.ConversationID, input.OffsetID, input.Limit, input.Direction, callerMemberPayload.FrozenMessageID)
	if errIndex != nil {
		numan_log.Error(ctx).Err(errIndex).Msg("Erreur lors de la résolution de l'index des messages")
		return nil, numan_error.NewInternal()
	}
	if len(messageIDsList) == 0 {
		return []message_models.MessageView{}, nil
	}

	// ── ÉTAPE 4 : HYDRATATION MASSIVE DES PAYLOADS (L1 -> L2 -> L3) ─────────

	messagesPayloadList, errHydration := object_cache_service.GetMessagesView(ctx, messageIDsList)
	if errHydration != nil {
		numan_log.Error(ctx).Err(errHydration).Msg("Erreur lors de l'hydratation massive des messages")
		return nil, numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : ASSEMBLAGE DES VUES (MÉDIAS, PSEUDOS & RÉACTIONS) ─────────

	hydratedMessageViews := make([]message_models.MessageView, 0, len(messagesPayloadList))

	for i := range messagesPayloadList {

		// FILTRE MÉTIER : Ignorer les messages systèmes si la conversation l'exige
		if mustHideSystemMessages && messagesPayloadList[i].MessageType == variables.MessageTypeSystem {
			continue
		}

		// A. Hydratation de l'image rattachée (Sceau Cryptographique HMAC)
		if messagesPayloadList[i].Attachments != nil {
			if rawMediaID, exists := messagesPayloadList[i].Attachments["media_id"]; exists {
				var targetMediaID int64
				switch parsedValue := rawMediaID.(type) {
				case float64:
					targetMediaID = int64(parsedValue)
				case int64:
					targetMediaID = parsedValue
				}

				if targetMediaID > 0 {
					if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, targetMediaID, messagesPayloadList[i].SenderID, messagesPayloadList[i].ID, callerID); errMedia == nil {
						messagesPayloadList[i].Attachments["media_view"] = mediaView
					}
				}
			}
		}

		// A.bis Hydratation du widget (Widget Message)
		if messagesPayloadList[i].MessageType == variables.MessageTypeWidget {
			HydrateWidgetMedia(ctx, &messagesPayloadList[i], callerID)
		}

		// B. Hydratation de l'expéditeur (Pseudo et Avatar)
		var resolvedSenderUsername string
		var resolvedSenderAvatar media_models.MediaView
		var resolvedSenderAvatarCommunityID int64

		if senderUserLite, errLite := cache_service.GetUserLite(ctx, messagesPayloadList[i].SenderID); errLite == nil {
			resolvedSenderUsername = senderUserLite.Username

			if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
				resolvedSenderAvatarCommunityID = senderUserLite.ProfilePictureID // Mode Twitch (Idéal pour grandes communautés)
			} else if senderUserLite.ProfilePictureID > 0 {
				// Mode Classique : URL HMAC
				if avatarView, errAvatar := media_service.GenerateMediaViewCascade(ctx, senderUserLite.ProfilePictureID, messagesPayloadList[i].SenderID, 0, callerID); errAvatar == nil {
					resolvedSenderAvatar = avatarView
				}
			}
		}

		// C. Hydratation des Réactions (Fast Path O(1) depuis RAM)
		reactionCountsMap, _ := cache_service.GetMessageReactionCounts(ctx, messagesPayloadList[i].ID)
		userSpecificReaction, _ := cache_service.GetUserReaction(ctx, messagesPayloadList[i].ID, callerID)

		// D. Assemblage de la vue finale
		hydratedMessageViews = append(hydratedMessageViews, message_models.MessageView{
			MessagePayload:          messagesPayloadList[i],
			SenderUsername:          resolvedSenderUsername,
			SenderAvatar:            resolvedSenderAvatar,
			SenderAvatarCommunityID: resolvedSenderAvatarCommunityID,
			ReactionCounts:          reactionCountsMap,
			UserReaction:            userSpecificReaction,
		})
	}

	return hydratedMessageViews, nil
}
