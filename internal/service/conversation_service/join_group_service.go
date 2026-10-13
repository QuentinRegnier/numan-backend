package conversation_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : REJOINDRE UN GROUPE / UNE COMMUNAUTÉ
// ############################################################################

// JoinGroup gère l'intégration d'un utilisateur à une conversation existante.
// Elle supporte 3 accès : Interne (Communauté), Externe (Lien URL), ou Invitation (MP).
func JoinGroup(ctx context.Context, callerID int64, input conversation_models.JoinGroupInput) (conversation_models.JoinGroupOutput, error) {

	// ── ÉTAPE 1 : AUTO-GUÉRISON ET CHARGEMENT DU GROUPE (L1 -> L2 -> L3) ────
	conversationPayload, errCache := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)

	if errCache != nil || conversationPayload.ID == 0 {
		var errMongo error
		conversationPayload, errMongo = mongo.MongoGetConversation(ctx, input.ConversationID)

		if errMongo != nil || conversationPayload.ID == 0 {
			var errPostgres error
			conversationPayload, errPostgres = postgres.FuncGetConversation(ctx, input.ConversationID)
			if errPostgres != nil || conversationPayload.ID == 0 {
				return conversation_models.JoinGroupOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "La conversation n'existe pas ou a été supprimée.", nil)
			}

			// PROMOTION L3 -> L2 (Mongo Asynchrone)
			go func(c conversation_models.ConversationPayload) {
				_ = redis.EnqueueDB(context.Background(), c.ID, c.ID, redis.EntityConversation, redis.ActionUpdate, c, redis.TargetMongo)
			}(conversationPayload)
		}

		// PROMOTION L3/L2 -> L1 (Redis Object Cache Immédiat)
		_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)
	}

	// ── ÉTAPE 2 : VÉRIFICATIONS MÉTIER DE BASE ───────────────────────────────
	if conversationPayload.Type == variables.ConversationTypeDirect {
		return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Impossible de rejoindre un message privé existant.", nil)
	}

	// ── ÉTAPE 3 : BARRIÈRE DE SÉCURITÉ (ROUTAGE D'ACCÈS) ─────────────────────
	if input.Internal {
		// Accès interne libre (Communauté)
		if conversationPayload.Type != variables.ConversationTypeCommunityPub && conversationPayload.Type != variables.ConversationTypeCommunityPriv {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seules les communautés peuvent être rejointes de manière interne.", nil)
		}
		if conversationPayload.Settings.JoinApprovalRequired {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Cette communauté nécessite une approbation, vous ne pouvez pas la rejoindre directement.", nil)
		}
	} else if input.External {
		// Accès par lien d'invitation externe (URL Web)
		if conversationPayload.Type != variables.ConversationTypeCommunityPub && conversationPayload.Type != variables.ConversationTypeCommunityPriv {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "L'accès via lien externe est réservé aux communautés.", nil)
		}
		if conversationPayload.Settings.JoinWithLinkDuration != 0 && domain.NowMillis() >= conversationPayload.Settings.JoinWithLinkDuration {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Le lien d'invitation a expiré.", nil)
		}
	} else {
		// Accès intra-plateforme par message d'invitation privé
		if input.InviteMsgID == 0 {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Une invitation est requise pour rejoindre ce groupe privé.", nil)
		}

		inviteMessagePayload, errSecurity := security_service.LeftMessage(ctx, input.InviteMsgID, callerID)
		if errSecurity != nil {
			// LeftMessage renvoie déjà une AppError formatée
			return conversation_models.JoinGroupOutput{}, errSecurity
		}

		if inviteMessagePayload.MessageType != variables.MessageTypeInvite {
			return conversation_models.JoinGroupOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le message fourni n'est pas une invitation valide.", nil)
		}

		if inviteMessagePayload.Attachments == nil {
			return conversation_models.JoinGroupOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'invitation est corrompue (aucune cible).", nil)
		}

		targetConvRaw, exists := inviteMessagePayload.Attachments["conversation_id"]
		if !exists {
			return conversation_models.JoinGroupOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'invitation est invalide (cible manquante).", nil)
		}

		var targetConversationID int64
		switch v := targetConvRaw.(type) {
		case float64:
			targetConversationID = int64(v)
		case int64:
			targetConversationID = v
		}

		if targetConversationID != input.ConversationID {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "L'invitation ne correspond pas à ce groupe.", nil)
		}
	}

	// ── ÉTAPE 4 : VÉRIFICATION DU STATUT DU MEMBRE (L1 -> L3) ───────────────
	var memberPayload member_models.MemberPayload
	var isAnUpdateOfExistingMember bool

	memberPayload, errCacheMem := object_cache_service.GetMemberFromObjectCache(ctx, input.ConversationID, callerID)
	if errCacheMem != nil || memberPayload.ID == 0 {
		memberPg, errPgMem := postgres.FuncGetMember(ctx, input.ConversationID, callerID)
		if errPgMem == nil && memberPg.ID != 0 {
			memberPayload = memberPg
		}
	}

	if memberPayload.ID != 0 {
		if memberPayload.Role == variables.MemberRoleBanned {
			return conversation_models.JoinGroupOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous avez été banni de ce groupe.", nil)
		}
		if memberPayload.Role == -3 { // État interne temporaire (En attente d'approbation)
			return conversation_models.JoinGroupOutput{}, numan_error.NewConflict(numan_error.CodeConflict, "Votre demande d'intégration est déjà en attente d'approbation.", nil)
		}
		if memberPayload.Role >= variables.MemberRoleNormal {
			return conversation_models.JoinGroupOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Vous faites déjà partie de ce groupe.", nil)
		}

		// Si l'utilisateur avait quitté (-1) ou a été refusé auparavant (-4),
		// il peut faire une nouvelle demande. C'est une simple mise à jour (UPDATE) et non une création.
		isAnUpdateOfExistingMember = true
	}

	// ── ÉTAPE 5 : VÉRIFICATION DES RÈGLES DE MODÉRATION ─────────────────────
	assignedRole := variables.MemberRoleNormal
	if conversationPayload.Settings.JoinApprovalRequired {
		assignedRole = variables.MemberRolePending // Statut temporaire : En attente
	}

	// ── ÉTAPE 6 : HYDRATATION DU MODÈLE DE DONNÉES ──────────────────────────
	currentTime := time.Now().UTC()

	if !isAnUpdateOfExistingMember {
		memberPayload = member_models.MemberPayload{
			ID:                pkg.GenerateID(),
			ConversationID:    input.ConversationID,
			UserID:            callerID,
			Role:              assignedRole,
			Settings:          member_models.DefaultMemberSettings(conversationPayload.Type),
			JoinedAt:          domain.TimeToMillis(currentTime),
			UnreadCount:       0,
			FrozenMessageID:   0,
			LastReadMessageID: 0,
			CreatedAt:         domain.TimeToMillis(currentTime),
			UpdatedAt:         domain.TimeToMillis(currentTime),
		}
	} else {
		memberPayload.Role = assignedRole
		memberPayload.JoinedAt = domain.TimeToMillis(currentTime)
		memberPayload.UpdatedAt = domain.TimeToMillis(currentTime)
		memberPayload.FrozenMessageID = 0
	}

	// ── ÉTAPE 7 : CACHE L1 INSTANTANÉ (UI SPEED CACHE) ──────────────────────
	_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)
	_ = cache_service.AddConversationToUserInbox(ctx, callerID, conversationPayload.ID, conversationPayload.LastMessageID)

	memberLiteRequest := lite_models.MemberLiteRequest{
		ConversationID:    memberPayload.ConversationID,
		UserID:            memberPayload.UserID,
		Role:              memberPayload.Role,
		Settings:          service.ToMemberSettingsLite(memberPayload.Settings),
		UnreadCount:       memberPayload.UnreadCount,
		FrozenMessageID:   memberPayload.FrozenMessageID,
		LastReadMessageID: memberPayload.LastReadMessageID,
		JoinedAt:          memberPayload.JoinedAt,
	}

	if isAnUpdateOfExistingMember {
		_ = cache_service.UpdateMemberSpeedCache(ctx, memberLiteRequest)
	} else {
		_ = cache_service.AddMemberToSpeedCache(ctx, memberLiteRequest)
	}

	// ── ÉTAPE 8 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	dbAction := redis.ActionCreate
	if isAnUpdateOfExistingMember {
		dbAction = redis.ActionUpdate
	}
	errQueue := redis.EnqueueDB(ctx, memberPayload.ID, input.ConversationID, redis.EntityMembers, dbAction, memberPayload, redis.TargetAll)
	if errQueue != nil {
		return conversation_models.JoinGroupOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 9 : ÉVÉNEMENTS SYSTÈMES ET WEBSOCKETS (ENTRÉE DIRECTE ONLY) ──
	if assignedRole == variables.MemberRoleNormal {
		go func() {
			backgroundContext := context.Background()

			if callerUserLite, errLite := cache_service.GetUserLite(backgroundContext, callerID); errLite == nil {

				// A. Publication du message système d'intégration
				callerUsername := "Unknown User"
				if errLite == nil {
					callerUsername = callerUserLite.Username
				}

				systemMessageInput := message_models.CreateMessageInput{
					MessageType: variables.MessageTypeSystem,
					Content:     "",
					Attachments: map[string]any{
						"sys_action": variables.SysActionMemberJoinedWithLink,
						"actor": map[string]any{
							"id":       callerID,
							"username": callerUsername,
						},
					},
				}
				_, _ = message_service.CreateMessage(backgroundContext, callerID, input.ConversationID, systemMessageInput, true)

				// B. Hydratation DTO pour le WebSocket
				memberView := member_models.MemberView{
					MemberPayload: memberPayload,
					Username:      callerUserLite.Username,
					IsOnline:      cache_service.IsUserOnline(backgroundContext, callerID),
				}

				if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
					memberView.AvatarCommunityID = callerUserLite.ProfilePictureID // Mode Twitch pour les commus
				} else if callerUserLite.ProfilePictureID > 0 {
					if mediaView, errMedia := media_service.GenerateMediaViewCascade(backgroundContext, callerUserLite.ProfilePictureID, callerID, 0, callerID); errMedia == nil {
						memberView.Avatar = mediaView
					}
				}

				// C. Diffusion WebSocket temps réel
				_ = realtime_service.BroadcastToConversation(backgroundContext, input.ConversationID, "member.joined", memberView)
			}
		}()
	}

	// ── ÉTAPE 10 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ─────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return conversation_models.JoinGroupOutput{
		InboxUpdateAt: domain.TimeToMillis(time.UnixMilli(latestActivityTimestampMs)),
	}, nil
}
