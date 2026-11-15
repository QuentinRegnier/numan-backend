package conversation_service

import (
	"context"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION D'UN LOT DE CONVERSATIONS
// ############################################################################

// GetConversations récupère les métadonnées fraîches d'un lot de conversations spécifiques.
func GetConversations(ctx context.Context, callerID int64, input conversation_models.GetConversationsInput) (conversation_models.GetInboxOutput, error) {
	if callerID <= 0 {
		return conversation_models.GetInboxOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return conversation_models.GetInboxOutput{}, err
	}

	conversationViews := make([]conversation_models.InboxConversationView, 0, len(input.ConversationIDs))
	callerIDString := strconv.FormatInt(callerID, 10)

	for _, targetConversationID := range input.ConversationIDs {

		// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS (SÉCURITÉ) ────────────────────────────

		// On ignore silencieusement les conversations inaccessibles ou inexistantes pour ne pas bloquer la boucle.
		callerMemberPayload, errSecurity := security_service.LeftMember(ctx, targetConversationID, callerID)
		if errSecurity != nil || callerMemberPayload.Role < variables.MemberRoleNormal {
			continue
		}

		// ── ÉTAPE 2 : CHARGEMENT DE LA CONVERSATION (CASCADE L1 -> L2 -> L3) ──

		conversationPayload, errCache := object_cache_service.GetConversationFromObjectCache(ctx, targetConversationID)

		if errCache != nil || conversationPayload.ID == 0 {
			var errMongo error
			conversationPayload, errMongo = mongo.MongoGetConversation(ctx, targetConversationID)

			if errMongo != nil || conversationPayload.ID == 0 {
				var errPostgres error
				conversationPayload, errPostgres = postgres.FuncGetConversation(ctx, targetConversationID)
				if errPostgres != nil || conversationPayload.ID == 0 {
					if errPostgres != nil {
						numan_log.Warn(ctx).Err(errPostgres).Int64("conv_id", targetConversationID).Msg("Échec fallback Postgres pour GetConversations")
					}
					continue // Échec total de la cascade, on passe à la conversation suivante
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

		// ── ÉTAPE 3 : HYDRATATION DYNAMIQUE (TITRE ET PRÉSENCE POUR MP) ───────

		displayTitle := conversationPayload.Title
		isUserOnline := false

		if conversationPayload.Type == variables.ConversationTypeDirect {
			participantsStringList, errParticipants := redis.ConvParticipants.SMembers(ctx, conversationPayload.ID)
			if errParticipants == nil {
				for _, participantStr := range participantsStringList {
					if participantStr != callerIDString {
						if otherParticipantID := pkg.ParseInt64(participantStr); otherParticipantID != 0 {
							// Extraction du pseudo de l'interlocuteur
							if otherUserLite, errLite := cache_service.GetUserLite(ctx, otherParticipantID); errLite == nil {
								displayTitle = otherUserLite.Username
							}
							// Vérification de sa présence en ligne
							isUserOnline = cache_service.IsUserOnline(ctx, otherParticipantID)
							break
						}
					}
				}
			}
		}

		// ── ÉTAPE 4 : CALCUL DES AVATARS (DÉLÉGATION) ────────────────────────

		conversationAvatars := getConversationAvatars(ctx, conversationPayload.ID, callerID, conversationPayload.Type)

		// ── ÉTAPE 5 : ASSEMBLAGE DE LA VUE DTO ───────────────────────────────

		conversationViews = append(conversationViews, conversation_models.InboxConversationView{
			ConversationID: conversationPayload.ID,
			Type:           conversationPayload.Type,
			Title:          displayTitle,
			Description:    conversationPayload.Description,
			AvatarID:       conversationPayload.AvatarID,
			LastMessageID:  conversationPayload.LastMessageID,
			ExternalLink:   conversationPayload.ExternalLink,
			Role:           callerMemberPayload.Role,
			Settings:       callerMemberPayload.Settings,
			UnreadCount:    callerMemberPayload.UnreadCount,
			Avatars:        conversationAvatars,
			IsOnline:       isUserOnline,
		})
	}

	// Prévention stricte du `null` en JSON
	if conversationViews == nil {
		conversationViews = make([]conversation_models.InboxConversationView, 0)
	}

	return conversation_models.GetInboxOutput{
		Conversations: conversationViews,
	}, nil
}
