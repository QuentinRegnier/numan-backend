package message_service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
)

// ############################################################################
// # SERVICE : CRÉATION D'UN MESSAGE
// ############################################################################

// CreateMessage construit le message, vérifie les droits d'écriture, active les
// médias orphelins, distribue l'événement en WebSocket et en Push FCM.
func CreateMessage(ctx context.Context, senderID int64, conversationID int64, input message_models.CreateMessageInput, isInternalCall bool) (message_models.CreateMessageOutput, error) {

	// ── ÉTAPE 1 : SÉCURITÉ SUR LES TYPES DE MESSAGES (ANTI-USURPATION) ──────

	if !isInternalCall {
		switch input.MessageType {
		case variables.MessageTypeText, variables.MessageTypeMedia, variables.MessageTypeGIF, variables.MessageTypeSurvey, variables.MessageTypeWidget:
			// Validés pour une entrée utilisateur
		case variables.MessageTypeVoice, variables.MessageTypeVideo:
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les messages vocaux et vidéos ne sont pas encore supportés par cette version.", nil)
		case variables.MessageTypePost, variables.MessageTypeInvite, variables.MessageTypeLink, variables.MessageTypeSystem:
			return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'avez pas l'autorisation d'émettre directement ce type de message système.", nil)
		default:
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de message inconnu.", nil)
		}
	}

	// ── ÉTAPE 2 : VÉRIFICATION DE LA DISCIPLINE DU MEMBRE ───────────────────

	senderMemberPayload, errSecurity := security_service.LeftMember(ctx, conversationID, senderID)
	if errSecurity != nil {
		return message_models.CreateMessageOutput{}, errSecurity
	}

	if senderMemberPayload.Role < variables.MemberRoleNormal {
		return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : Vous êtes banni ou vous ne faites plus partie de cette conversation.", nil)
	}

	// Le Videur Intraitable : Vérification des restrictions de parole (Mute)
	currentTimeMs := domain.NowMillis()
	if senderMemberPayload.Settings.RestrictedUntil > currentTimeMs {
		return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous êtes actuellement restreint et ne pouvez pas parler dans ce groupe.", nil)
	}

	// Lazy Evaluation : Si le mute est expiré mais toujours présent en BDD, on guérit le membre à la volée.
	if senderMemberPayload.Settings.RestrictedUntil > 0 && senderMemberPayload.Settings.RestrictedUntil <= currentTimeMs {
		senderMemberPayload.Settings.RestrictedUntil = 0
		senderMemberPayload.UpdatedAt = currentTimeMs

		compositeMemberID := fmt.Sprintf("%d:%d", conversationID, senderID)
		var liteMemberRequest lite_models.MemberLiteRequest
		if errMem := redis.ConvMembers.GetObject(ctx, compositeMemberID, &liteMemberRequest); errMem == nil {
			liteMemberRequest.Settings.RestrictedUntil = 0
			_ = redis.ConvMembers.SetObject(ctx, compositeMemberID, liteMemberRequest)
		}

		_ = redis.EnqueueDB(ctx, senderMemberPayload.ID, conversationID, redis.EntityMembers, redis.ActionUpdate, senderMemberPayload, redis.TargetAll)
		_ = realtime_service.BroadcastToConversation(context.Background(), conversationID, "member.unmuted", senderMemberPayload)
	}

	// ── ÉTAPE 3 : RÉCUPÉRATION ET RÈGLES DE LA CONVERSATION (CASCADE) ───────

	conversationPayload, errCache := object_cache_service.GetConversationFromObjectCache(ctx, conversationID)

	if errCache != nil || conversationPayload.ID == 0 {
		var errMongo error
		conversationPayload, errMongo = mongo.MongoGetConversation(ctx, conversationID)

		if errMongo != nil || conversationPayload.ID == 0 {
			var errPg error
			conversationPayload, errPg = postgres.FuncGetConversation(ctx, conversationID)
			if errPg != nil || conversationPayload.ID == 0 {
				numan_log.Error(ctx).Err(errPg).Int64("conv_id", conversationID).Msg("Erreur L3 : Conversation introuvable lors de CreateMessage")
				return message_models.CreateMessageOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "La conversation ciblée est introuvable.", errPg)
			}

			// PROMOTION L3 -> L2 (Asynchrone via Worker)
			go func(c conversation_models.ConversationPayload) {
				bgCtx := context.Background()
				_ = redis.EnqueueDB(bgCtx, c.ID, c.ID, redis.EntityConversation, redis.ActionUpdate, c, redis.TargetMongo)
			}(conversationPayload)
		}

		// PROMOTION L3/L2 -> L1
		_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)
	}

	// Application des Permissions (Groups & Communities)
	if input.MessageType == variables.MessageTypeSurvey && (conversationPayload.Type < variables.ConversationTypeGroup || !conversationPayload.Settings.SendSurveyPermission) {
		return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Les sondages sont désactivés ou non supportés dans cette conversation.", nil)
	}

	if input.MessageType == variables.MessageTypeWidget {
		// Règle A : La conversation doit être une communauté publique (Type 3)
		if conversationPayload.Type != variables.ConversationTypeCommunityPub {
			return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Les messages widgets ne sont autorisés que dans les communautés publiques.", nil)
		}

		// Règle B : L'expéditeur doit être administrateur ou propriétaire (Role >= 1)
		if senderMemberPayload.Role < variables.MemberRoleAdmin {
			return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs peuvent envoyer des messages widgets.", nil)
		}
	}

	if !isInternalCall && conversationPayload.Type > variables.ConversationTypeDirect {

		// 1 : Seuls les Admins peuvent écrire.
		if conversationPayload.Settings.WritePermission == 1 && senderMemberPayload.Role == variables.MemberRoleNormal {
			return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs peuvent envoyer des messages dans ce canal.", nil)
		}

		// 2 : Les membres ne peuvent que répondre aux Threads.
		if conversationPayload.Settings.WritePermission == 2 && senderMemberPayload.Role == variables.MemberRoleNormal {
			if input.ThreadParentID == 0 {
				return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Dans cette communauté, vous ne pouvez que répondre aux annonces existantes.", nil)
			}
		}

		if input.MessageType == variables.MessageTypeMedia && !conversationPayload.Settings.SendMediaPermission && senderMemberPayload.Role == variables.MemberRoleNormal {
			return message_models.CreateMessageOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'êtes pas autorisé à envoyer des médias dans ce groupe.", nil)
		}
	}

	// ── ÉTAPE 4 : ACTIVATION DU MÉDIA (OUT-OF-BAND) ET NETTOYAGE ────────────

	messageAttachments := input.Attachments
	if messageAttachments == nil {
		messageAttachments = make(map[string]any)
	}

	var messageMediaIAScore float64
	if input.MessageType == variables.MessageTypeWidget {
		rawWidget, hasWidget := messageAttachments["widget"]
		if !hasWidget || rawWidget == nil {
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le widget est manquant dans les pièces jointes.", nil)
		}

		widgetPayload, errParse := message_models.ParseWidgetPayload(rawWidget)
		if errParse != nil {
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload du widget est invalide.", errParse)
		}

		widgetPayload.Type = strings.TrimSpace(widgetPayload.Type)
		if !slices.Contains(variables.AllowedWidgetTypes, widgetPayload.Type) {
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de widget non supporté.", nil)
		}

		// Assainissement des chaînes fournies par l'utilisateur
		widgetPayload.Title = pkg.CleanStr(widgetPayload.Title)
		widgetPayload.Description = pkg.CleanStr(widgetPayload.Description)
		widgetPayload.Action.Label = pkg.CleanStr(widgetPayload.Action.Label)
		widgetPayload.Action.URL = pkg.CleanStr(widgetPayload.Action.URL)
		widgetPayload.Visuals.Style = pkg.CleanStr(widgetPayload.Visuals.Style)
		widgetPayload.Visuals.Icon = pkg.CleanStr(widgetPayload.Visuals.Icon)

		// Activation facultative du média de couverture
		if widgetPayload.Visuals.CoverMediaID > 0 {
			if errActivation := media_service.ActivateMediaBatch(ctx, []int64{widgetPayload.Visuals.CoverMediaID}, senderID); errActivation != nil {
				return message_models.CreateMessageOutput{}, errActivation
			}
		}

		// Réécriture des données assainies dans les pièces jointes
		messageAttachments["widget"] = message_models.WidgetPayloadToMap(widgetPayload)

		input.Content = pkg.CleanStr(input.Content)
	} else if input.MessageType == variables.MessageTypeMedia {
		rawMediaID, hasMediaID := messageAttachments["media_id"]
		if !hasMediaID {
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média est manquant dans les pièces jointes.", nil)
		}

		var uploadedMediaID int64
		switch v := rawMediaID.(type) {
		case float64:
			uploadedMediaID = int64(v)
		case int64:
			uploadedMediaID = v
		}

		if uploadedMediaID > 0 {
			mediaIAScore, errActivation := media_service.ActivateMediaForMessage(ctx, uploadedMediaID, senderID)
			if errActivation != nil {
				return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de valider ce média.", errActivation)
			}
			messageMediaIAScore = mediaIAScore
		} else {
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média est invalide.", nil)
		}

		// Si le client Flutter a transmis directement ia_score dans attachments, on retient le max
		if rawIAScore, hasIA := messageAttachments["ia_score"].(float64); hasIA {
			messageMediaIAScore = moderation_service.AggregateMaxIAScore(messageMediaIAScore, rawIAScore)
		}
		if messageMediaIAScore > 0 {
			messageAttachments["ia_score"] = messageMediaIAScore
		}
		input.Content = pkg.CleanStr(input.Content)
	} else {
		input.Content = pkg.CleanStr(input.Content)
		if input.Content == "" && input.MessageType == variables.MessageTypeText {
			return message_models.CreateMessageOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un message texte ne peut pas être vide.", nil)
		}
	}

	// Extraction O(N) des mentions d'utilisateurs (@{ID}) validées.
	validatedMentionUserIDs := pkg.ExtractMentions(input.Content)

	// ── ÉTAPE 5 : PRÉPARATION DU PAYLOAD ET CACHE IMMÉDIAT (L1) ─────────────
	newMessageID := pkg.GenerateID()
	currentTimestamp := time.Now().UTC()

	// Si le message comporte une image (MessageTypeMedia), évaluation combinée (P_ia, T_auteur)
	// pour émettre un signalement automatique Robot si le seuil de risque est dépassé.
	if input.MessageType == variables.MessageTypeMedia && messageMediaIAScore > 0 {
		senderTrustScore := variables.TrustScoreInitial
		if senderLite, errLite := cache_service.GetUserLite(ctx, senderID); errLite == nil && senderLite.TrustScore > 0 {
			senderTrustScore = senderLite.TrustScore
		}
		_, _, _ = moderation_service.EvaluateAndTriggerAutoReport(
			ctx, senderID, senderTrustScore, variables.ReportTargetMessage, newMessageID, messageMediaIAScore,
		)
	}

	allowNewReactions := true
	if !variables.ShouldAllowNewReactions(input.MessageType) {
		allowNewReactions = false
	}

	messagePayload := message_models.MessagePayload{
		ID:                newMessageID,
		ConversationID:    conversationID,
		SenderID:          senderID,
		MessageType:       input.MessageType,
		Visibility:        true,
		Content:           input.Content,
		Attachments:       messageAttachments,
		AllowNewReactions: allowNewReactions,
		CreatedAt:         domain.TimeToMillis(currentTimestamp),
		UpdatedAt:         domain.TimeToMillis(currentTimestamp),
	}

	_ = object_cache_service.SetMessageInObjectCache(ctx, messagePayload)

	// Cette fonction met à jour les non-lus, remonte la conversation dans les Inboxes de tous les participants,
	// et retourne la liste des destinataires valides pour l'envoi WS et Push.
	recipientUserIDsList, _ := cache_service.ProcessNewMessageInSpeedCache(ctx, newMessageID, conversationID, senderID)

	// Rechargement léger de la conversation pour avoir les données fraîches pour le WebSocket.
	conversationPayload, _ = object_cache_service.GetConversationFromObjectCache(ctx, conversationID)

	// ── ÉTAPE 6 : DISTRIBUTION TEMPS RÉEL (WEBSOCKETS) ──────────────────────

	messageViewDto := message_models.MessageView{
		MessagePayload: messagePayload,
	}

	if senderUserLite, errLite := cache_service.GetUserLite(ctx, senderID); errLite == nil {
		messageViewDto.SenderUsername = senderUserLite.Username

		if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
			messageViewDto.SenderAvatarCommunityID = senderUserLite.ProfilePictureID // Mode Twitch sans signatures pour scaler
		} else if senderUserLite.ProfilePictureID > 0 {
			if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, senderUserLite.ProfilePictureID, senderID, 0, senderID); errMedia == nil {
				messageViewDto.SenderAvatar = mediaView
			}
		}
	}

	if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
		_ = realtime_service.DistributeToCommunity(ctx, variables.NotificationMessageCreated, messageViewDto, conversationID)
	} else {
		_ = realtime_service.DistributeToUsers(ctx, variables.NotificationMessageCreated, messageViewDto, recipientUserIDsList)
	}

	// ── ÉTAPE 7 : DÉLÉGATION FCM (PUSH NOTIFICATIONS) ET WORKERS ────────────

	dispatchPushNotifications(messageViewDto, recipientUserIDsList, validatedMentionUserIDs)

	// Envoi des instructions d'incrémentations au compteur asynchrone pour ne pas ralentir le serveur HTTP.
	for _, recipientID := range recipientUserIDsList {
		worker.RegisterUnread(conversationID, recipientID)
	}

	errQueue := redis.EnqueueDB(ctx, newMessageID, conversationID, redis.EntityMessage, redis.ActionCreate, messagePayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("msg_id", newMessageID).Msg("Échec critique du Write-Behind pour la création d'un message")
		return message_models.CreateMessageOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 8 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, senderID)

	return message_models.CreateMessageOutput{
		MessageID:     newMessageID,
		InboxUpdateAt: domain.TimeToMillis(time.UnixMilli(latestActivityTimestampMs)),
	}, nil
}
