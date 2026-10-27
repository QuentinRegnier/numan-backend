package conversation_service

import (
	"context"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DE L'INBOX PAGINÉE (CASCADE L1 -> L2 -> L3)
// ############################################################################

// GetUserConversationsPaginated récupère l'inbox avec une cascade complète et promotion en mémoire.
func GetUserConversationsPaginated(ctx context.Context, callerID int64, input conversation_models.GetUserConversationsInput) (conversation_models.GetUserInboxOutput, error) {
	var err_offset, err_limit numan_error.Error
	input.Offset, err_offset, input.Limit, err_limit = pkg.BatchVerif(input.Offset, input.Limit)
	if err_offset != nil || err_limit != nil {
		return conversation_models.GetUserInboxOutput{}, numan_error.Combine(err_offset, err_limit)
	}
	var rawInboxItems []cache_service.InboxItemView

	// ── ÉTAPE 0 : GESTION DU MODE FORCE (PURGE RAM L1) ──────────────────────
	if input.Force {
		_ = cache_service.PurgeUserConversations(ctx, callerID)
	}

	// ── ÉTAPE 1 : TENTATIVE L1 (SPEED CACHE) ────────────────────────────────
	// Réservé aux premiers éléments (< 100) et hors mode Forcé pour protéger la RAM.
	if input.Offset < int64(variables.MaxZsetInbox) && !input.Force {
		cachedItems, errCache := cache_service.GetInboxView(ctx, callerID, input.Limit, input.Offset)
		if errCache == nil && len(cachedItems) > 0 {
			rawInboxItems = cachedItems
		}
	}

	// ── ÉTAPE 2 : FALLBACK L2 (MONGODB - WARM STORAGE) ──────────────────────
	if len(rawInboxItems) == 0 {
		mongoConversations, errMongo := mongo.MongoLoadConversationPaginated(callerID, input.Limit, input.Offset)
		if errMongo == nil && len(mongoConversations) > 0 {
			for _, record := range mongoConversations {
				rawInboxItems = append(rawInboxItems, cache_service.InboxItemView{
					Conversation: lite_models.ConvLiteRequest{
						ID:            record.Conversation.ID,
						Type:          record.Conversation.Type,
						Title:         record.Conversation.Title,
						Description:   record.Conversation.Description,
						AvatarID:      record.Conversation.AvatarID,
						LastMessageID: record.Conversation.LastMessageID,
						Settings:      service.ToConversationSettingsLite(record.Conversation.Settings),
						ExternalLink:  record.Conversation.ExternalLink,
					},
					Member: lite_models.MemberLiteRequest{
						ConversationID:    record.Member.ConversationID,
						UserID:            record.Member.UserID,
						Role:              record.Member.Role,
						Settings:          service.ToMemberSettingsLite(record.Member.Settings),
						FrozenMessageID:   record.Member.FrozenMessageID,
						LastReadMessageID: record.Member.LastReadMessageID,
						UnreadCount:       record.Member.UnreadCount,
						JoinedAt:          record.Member.JoinedAt,
					},
				})

				// AUTO-GUÉRISON : Hit Mongo L2 -> Réhydratation Redis L1 (Object et Speed)
				go func(fullConv conversation_models.ConversationPayload, fullMember member_models.MemberPayload, offsetVal int64) {
					bgCtx := context.Background()
					_ = object_cache_service.SetConversationInObjectCache(bgCtx, fullConv)
					_ = object_cache_service.SetMemberInObjectCache(bgCtx, fullMember)
					cache_service.RehydrateConversationItemInSpeedCache(bgCtx, fullConv, fullMember, offsetVal)
				}(record.Conversation, record.Member, input.Offset)
			}
		}
	}

	// ── ÉTAPE 3 : FALLBACK ULTIME L3 (POSTGRESQL - SOURCE DE VÉRITÉ) ────────
	if len(rawInboxItems) == 0 {
		postgresConversations, errPostgres := postgres.FuncLoadConversationPaginated(ctx, callerID, input.Limit, input.Offset)
		if errPostgres != nil {
			numan_log.Error(ctx).Err(errPostgres).Int64("user_id", callerID).Msg("Erreur L3 lors du chargement de l'inbox")
			return conversation_models.GetUserInboxOutput{}, numan_error.NewInternal() // Erreur SQL protégée
		}

		for _, record := range postgresConversations {
			rawInboxItems = append(rawInboxItems, cache_service.InboxItemView{
				Conversation: lite_models.ConvLiteRequest{
					ID:            record.Conversation.ID,
					Type:          record.Conversation.Type,
					Title:         record.Conversation.Title,
					Description:   record.Conversation.Description,
					AvatarID:      record.Conversation.AvatarID,
					LastMessageID: record.Conversation.LastMessageID,
					Settings:      service.ToConversationSettingsLite(record.Conversation.Settings),
					ExternalLink:  record.Conversation.ExternalLink,
				},
				Member: lite_models.MemberLiteRequest{
					ConversationID:    record.Member.ConversationID,
					UserID:            record.Member.UserID,
					Role:              record.Member.Role,
					Settings:          service.ToMemberSettingsLite(record.Member.Settings),
					FrozenMessageID:   record.Member.FrozenMessageID,
					LastReadMessageID: record.Member.LastReadMessageID,
					UnreadCount:       record.Member.UnreadCount,
					JoinedAt:          record.Member.JoinedAt,
				},
			})

			// PROMOTION L3 -> L2 (Mongo) & L1 (Redis)
			go func(fullConv conversation_models.ConversationPayload, fullMember member_models.MemberPayload, offsetVal int64) {
				bgCtx := context.Background()

				// A. Persistance asynchrone Mongo L2
				_ = redis.EnqueueDB(bgCtx, fullConv.ID, fullConv.ID, redis.EntityConversation, redis.ActionUpdate, fullConv, redis.TargetMongo)
				_ = redis.EnqueueDB(bgCtx, fullMember.ID, fullMember.ConversationID, redis.EntityMembers, redis.ActionUpdate, fullMember, redis.TargetMongo)

				// B. Hydratation L1 Object Cache & Speed Cache
				_ = object_cache_service.SetConversationInObjectCache(bgCtx, fullConv)
				_ = object_cache_service.SetMemberInObjectCache(bgCtx, fullMember)
				cache_service.RehydrateConversationItemInSpeedCache(bgCtx, fullConv, fullMember, offsetVal)
			}(record.Conversation, record.Member, input.Offset)
		}
	}

	// ── ÉTAPE 4 : MAPPING ET ENRICHISSEMENT VERS LE MODÈLE PUBLIC ───────────
	finalInboxView := make([]conversation_models.InboxConversationView, 0, len(rawInboxItems))
	callerIDString := strconv.FormatInt(callerID, 10)

	for _, item := range rawInboxItems {
		conversationAvatars := getConversationAvatars(ctx, item.Conversation.ID, callerID, item.Conversation.Type)

		displayTitle := item.Conversation.Title
		isUserOnline := false

		// Résolution de l'interlocuteur pour les messages privés
		if item.Conversation.Type == variables.ConversationTypeDirect {
			participantsStringList, errParticipants := redis.ConvParticipants.SMembers(ctx, item.Conversation.ID)
			if errParticipants == nil {
				for _, participantStr := range participantsStringList {
					if participantStr != callerIDString {
						if otherParticipantID := pkg.ParseInt64(participantStr); otherParticipantID != 0 {
							if otherUserLite, errLite := cache_service.GetUserLite(ctx, otherParticipantID); errLite == nil {
								displayTitle = otherUserLite.Username
							}
							isUserOnline = cache_service.IsUserOnline(ctx, otherParticipantID)
							break
						}
					}
				}
			}
		}

		finalInboxView = append(finalInboxView, conversation_models.InboxConversationView{
			ConversationID: item.Conversation.ID,
			Type:           item.Conversation.Type,
			Title:          displayTitle,
			Description:    item.Conversation.Description,
			AvatarID:       item.Conversation.AvatarID,
			LastMessageID:  item.Conversation.LastMessageID,
			Role:           item.Member.Role,
			Settings:       service.ToDomainMemberSettings(item.Member.Settings),
			ExternalLink:   item.Conversation.ExternalLink,
			UnreadCount:    item.Member.UnreadCount,
			Avatars:        conversationAvatars,
			IsOnline:       isUserOnline,
		})
	}

	// Renvoie un tableau vide plutôt que 'null' en JSON si pas de conversation
	if finalInboxView == nil {
		finalInboxView = make([]conversation_models.InboxConversationView, 0)
	}

	return conversation_models.GetUserInboxOutput{
		Conversations: finalInboxView,
	}, nil
}
