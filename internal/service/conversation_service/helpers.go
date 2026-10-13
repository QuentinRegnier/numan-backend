package conversation_service

import (
	"context"
	"fmt"
	"sort"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
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
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # UTILITAIRES : GESTION DES MESSAGES PRIVÉS (DIRECT MESSAGES)
// ############################################################################

// getOrCreateDirectConversation gère la cascade L1 -> L2 -> L3 pour trouver un Message Privé (MP) existant entre deux utilisateurs,
// ou déclenche sa création formelle s'il n'existe pas encore.
func getOrCreateDirectConversation(ctx context.Context, callerID, targetID int64) (int64, error) {

	// ── ÉTAPE 1 : TENTATIVE L1 (Speed Cache O(n)) ───────────────────────────
	if cachedConversationID, errCache := cache_service.GetDirectConversationCache(ctx, callerID, targetID); errCache == nil && cachedConversationID > 0 {
		return cachedConversationID, nil
	}

	// ── ÉTAPE 2 : TENTATIVE L2 (MongoDB - Warm Storage) ─────────────────────
	if mongoConversation, errMongo := mongo.MongoGetDirectConversation(ctx, callerID, targetID); errMongo == nil && mongoConversation.ID > 0 {
		hydrateConversationCascade(ctx, mongoConversation, callerID, targetID, false)
		return mongoConversation.ID, nil
	}

	// ── ÉTAPE 3 : TENTATIVE L3 (PostgreSQL - Cold Storage) ──────────────────
	if pgConversation, errPg := postgres.FuncGetDirectConversation(ctx, callerID, targetID); errPg == nil && pgConversation.ID > 0 {
		hydrateConversationCascade(ctx, pgConversation, callerID, targetID, true)
		return pgConversation.ID, nil
	}

	// ── ÉTAPE 4 : CRÉATION (Si introuvable sur toute la ligne) ──────────────
	creationInput := conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeDirect, // Type 0 = Message Privé
		ParticipantIDs: []int64{targetID},
	}

	// Extraction de l'ID depuis l'Output de CreateConversation
	creationOutput, errCreate := CreateConversation(ctx, callerID, creationInput)
	if errCreate != nil {
		return 0, errCreate
	}

	return creationOutput.ConversationID, nil
}

// hydrateConversationCascade gère l'auto-guérison croisée des caches (L1 et L2) suite à un fallback L2 ou L3.
func hydrateConversationCascade(ctx context.Context, conversationPayload conversation_models.ConversationPayload, callerID, targetID int64, isFromColdStorage bool) {

	// Promotion vers MongoDB si la donnée provient de PostgreSQL
	if isFromColdStorage {
		go func(c conversation_models.ConversationPayload) {
			_ = redis.EnqueueDB(context.Background(), c.ID, c.ID, redis.EntityConversation, redis.ActionUpdate, c, redis.TargetMongo)
		}(conversationPayload)
	}

	// Guérison de l'Object Cache L1 (Données brutes)
	_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)

	// Guérison des Métadonnées (Speed Cache L1)
	liteConversationRequest := lite_models.ConvLiteRequest{
		ID:            conversationPayload.ID,
		Type:          conversationPayload.Type,
		Title:         conversationPayload.Title,
		Description:   conversationPayload.Description,
		AvatarID:      conversationPayload.AvatarID,
		LastMessageID: conversationPayload.LastMessageID,
		Settings:      service.ToConversationSettingsLite(conversationPayload.Settings),
		ExternalLink:  conversationPayload.ExternalLink,
	}
	_ = redis.ConvMeta.SetObject(ctx, conversationPayload.ID, liteConversationRequest)

	// Guérison asynchrone/synchrone des membres de la conversation
	hydrateMember(ctx, conversationPayload.ID, callerID, isFromColdStorage)
	hydrateMember(ctx, conversationPayload.ID, targetID, isFromColdStorage)

	// Guérison du ZSET Inbox de l'utilisateur via la couche d'abstraction (DDD)
	_ = cache_service.AddConversationToUserInbox(ctx, callerID, conversationPayload.ID, conversationPayload.LastMessageID)
	_ = cache_service.AddConversationToUserInbox(ctx, targetID, conversationPayload.ID, conversationPayload.LastMessageID)
}

// hydrateMember gère l'auto-guérison croisée d'un membre spécifique.
func hydrateMember(ctx context.Context, conversationID, userID int64, isFromColdStorage bool) {
	var memberPayload member_models.MemberPayload
	var errFetch error

	if isFromColdStorage {
		memberPayload, errFetch = postgres.FuncGetMember(ctx, conversationID, userID)
		if errFetch == nil {
			// Réhydratation L2 Asynchrone
			go func(m member_models.MemberPayload) {
				_ = redis.EnqueueDB(context.Background(), m.ID, m.ConversationID, redis.EntityMembers, redis.ActionUpdate, m, redis.TargetMongo)
			}(memberPayload)
		}
	} else {
		memberPayload, errFetch = mongo.MongoGetMember(ctx, conversationID, userID)
	}

	if errFetch == nil && memberPayload.ID != 0 {
		_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)
		_ = cache_service.AddMemberToSpeedCache(ctx, lite_models.MemberLiteRequest{
			ConversationID:    memberPayload.ConversationID,
			UserID:            memberPayload.UserID,
			Role:              memberPayload.Role,
			Settings:          service.ToMemberSettingsLite(memberPayload.Settings),
			FrozenMessageID:   memberPayload.FrozenMessageID,
			LastReadMessageID: memberPayload.LastReadMessageID,
			UnreadCount:       memberPayload.UnreadCount,
			JoinedAt:          memberPayload.JoinedAt,
		})
	}
}

// ############################################################################
// # UTILITAIRES : CALCUL DES AVATARS DE CONVERSATION
// ############################################################################

// candidate représente un profil potentiellement éligible pour être l'avatar d'une conversation de groupe.
type candidate struct {
	UserID   int64
	Role     int
	JoinedAt int64
	Relation int
}

// getConversationAvatars calcule les avatars à afficher pour une conversation en respectant la hiérarchie sociale
// (Propriétaire d'abord, puis amis/abonnés, puis par ancienneté).
func getConversationAvatars(ctx context.Context, conversationID int64, callerID int64, conversationType int) []media_models.MediaView {
	var avatarsToDisplay []media_models.MediaView

	// 1. Récupération des participants en O(1) via Set Redis
	participantsStringList, errRedis := redis.ConvParticipants.SMembers(ctx, conversationID)
	if errRedis != nil || len(participantsStringList) == 0 {
		return avatarsToDisplay
	}

	participantIDs := pkg.ParseInt64List(participantsStringList)

	// ── CAS A : Message Privé (Type 0) -> Image du correspondant ────────────
	if conversationType == variables.ConversationTypeDirect && len(participantIDs) == 2 {
		for _, pID := range participantIDs {
			if pID != callerID {
				return fetchAvatarsForUsers(ctx, []int64{pID}, conversationID, callerID)
			}
		}
	}

	// ── CAS D : Groupe avec seulement 2 personnes -> Image des deux membres ─
	if conversationType > variables.ConversationTypeDirect && len(participantIDs) == 2 {
		return fetchAvatarsForUsers(ctx, participantIDs, conversationID, callerID)
	}

	// ── CAS B & C : Groupes et Communautés (Tri Complexe RAM) ───────────────

	// A. MGET sur ConvMembers pour extraire l'ancienneté et le rôle de chacun
	var memberRedisKeys []any
	for _, pID := range participantIDs {
		memberRedisKeys = append(memberRedisKeys, fmt.Sprintf("%d:%d", conversationID, pID))
	}
	membersValuesBatch, _ := redis.ConvMembers.MGet(ctx, memberRedisKeys...)

	// B. Création de la liste des candidats et identification du propriétaire
	var avatarCandidates []candidate
	var ownerUserID int64

	for _, rawValue := range membersValuesBatch {
		if rawValue == nil {
			continue
		}
		if stringValue, ok := rawValue.(string); ok {
			var liteMember lite_models.MemberLiteRequest
			if msgpack.Unmarshal([]byte(stringValue), &liteMember) == nil {

				if liteMember.Role == variables.MemberRoleOwner {
					ownerUserID = liteMember.UserID
				}

				// On récupère la relation (2 = Ami, 1 = Abonné, 0 = Rien)
				// Sur 50 membres, la boucle O(1) de L1 prendra < 1ms.
				relationScore := cache_service.RelationValue(ctx, liteMember.UserID, callerID)

				avatarCandidates = append(avatarCandidates, candidate{
					UserID:   liteMember.UserID,
					Role:     liteMember.Role,
					JoinedAt: liteMember.JoinedAt,
					Relation: relationScore,
				})
			}
		}
	}

	// C. TRI EN RAM (Priorité 1: Relation DESC, Priorité 2: Ancienneté ASC)
	sort.Slice(avatarCandidates, func(i, j int) bool {
		if avatarCandidates[i].Relation != avatarCandidates[j].Relation {
			return avatarCandidates[i].Relation > avatarCandidates[j].Relation // 2 > 1 > 0
		}
		return avatarCandidates[i].JoinedAt < avatarCandidates[j].JoinedAt // Les fondateurs d'abord
	})

	// D. SÉLECTION DES GAGNANTS (Qui apparaîtra sur la bulle de chat ?)
	var selectedUserIDs []int64

	if callerID == ownerUserID {
		// L'utilisateur est le propriétaire -> Afficher 2 images du haut du classement (hors lui-même)
		for _, candidate := range avatarCandidates {
			if candidate.UserID != callerID {
				selectedUserIDs = append(selectedUserIDs, candidate.UserID)
			}
			if len(selectedUserIDs) == 2 {
				break
			}
		}
	} else {
		// L'utilisateur n'est pas le propriétaire -> Afficher le Proprio + 1 image du classement
		if ownerUserID != 0 {
			selectedUserIDs = append(selectedUserIDs, ownerUserID)
		}

		for _, candidate := range avatarCandidates {
			// Le second membre retenu ne doit être ni le proprio ni l'appelant
			if candidate.UserID != ownerUserID && candidate.UserID != callerID {
				selectedUserIDs = append(selectedUserIDs, candidate.UserID)
				break // On n'en veut qu'un seul
			}
		}
	}

	return fetchAvatarsForUsers(ctx, selectedUserIDs, conversationID, callerID)
}

// fetchAvatarsForUsers résout l'Object Cache Média et signe cryptographiquement (HMAC) les URL en un éclair.
func fetchAvatarsForUsers(ctx context.Context, targetUserIDs []int64, conversationID int64, callerID int64) []media_models.MediaView {
	var generatedAvatars []media_models.MediaView
	if len(targetUserIDs) == 0 {
		return generatedAvatars
	}

	userLiteBatchResult, errBatch := redis.UsersLite.GetMany(ctx, targetUserIDs)
	if errBatch != nil {
		return generatedAvatars
	}

	// On boucle sur targetUserIDs pour conserver rigoureusement l'ordre du classement
	for _, targetUID := range targetUserIDs {
		if rawData, isFound := userLiteBatchResult.Found[targetUID]; isFound {
			var liteUser lite_models.UserLiteRequest
			if msgpack.Unmarshal(rawData, &liteUser) == nil && liteUser.ProfilePictureID > 0 {

				// Signature instantanée via le domaine Média
				if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, liteUser.ProfilePictureID, liteUser.ID, conversationID, callerID); errMedia == nil {
					generatedAvatars = append(generatedAvatars, mediaView)
				}
			}
		}
	}

	// Évite le retour null dans les réponses JSON
	if generatedAvatars == nil {
		generatedAvatars = make([]media_models.MediaView, 0)
	}
	return generatedAvatars
}

// ############################################################################
// # ROUTINES DE FOND (BACKGROUND WORKERS LOCAUX)
// ############################################################################

// acceptAllPendingMembers pagine et intègre tous les membres en attente (Rôle = -3)
// lors de la désactivation du mode "Approbation Requise" dans une communauté.
func acceptAllPendingMembers(ctx context.Context, conversationID int64, callerAdminID int64) {
	for {
		var pendingMembersPayloads []member_models.MemberPayload

		// TENTATIVE L2 (Warm Storage MongoDB)
		membersFromMongo, errMongo := mongo.MongoLoadMembersByRolePaginated(conversationID, variables.MemberRolePending, int64(variables.FetchBatchLimit), 0)
		if errMongo == nil && len(membersFromMongo) > 0 {
			pendingMembersPayloads = membersFromMongo
		} else {
			// FALLBACK L3 (Cold Storage PostgreSQL)
			membersFromPostgres, errPg := postgres.FuncLoadMembersByRolePaginated(ctx, conversationID, variables.MemberRolePending, int64(variables.FetchBatchLimit), 0)
			if errPg == nil && len(membersFromPostgres) > 0 {
				pendingMembersPayloads = membersFromPostgres
			} else if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("conv_id", conversationID).Msg("Échec L3 de récupération des membres en attente d'approbation")
				break
			}
		}

		// Condition d'arrêt de la boucle d'aspiration (Plus aucun membre à intégrer)
		if len(pendingMembersPayloads) == 0 {
			break
		}

		// Traitement de l'intégration pour ce batch
		for _, targetMemberPayload := range pendingMembersPayloads {
			currentTimeMs := domain.NowMillis()
			targetMemberPayload.Role = variables.MemberRoleNormal // Devient officiellement membre actif
			targetMemberPayload.Settings = member_models.DefaultMemberSettings(variables.ConversationTypeCommunityPub)
			targetMemberPayload.JoinedAt = currentTimeMs
			targetMemberPayload.UpdatedAt = currentTimeMs

			// 1. MISE À JOUR SYNCHRONE DES CACHES RAM L1
			_ = object_cache_service.SetMemberInObjectCache(ctx, targetMemberPayload)

			liteMemberRequest := lite_models.MemberLiteRequest{
				ConversationID:    targetMemberPayload.ConversationID,
				UserID:            targetMemberPayload.UserID,
				Role:              targetMemberPayload.Role,
				Settings:          service.ToMemberSettingsLite(targetMemberPayload.Settings),
				UnreadCount:       targetMemberPayload.UnreadCount,
				FrozenMessageID:   targetMemberPayload.FrozenMessageID,
				LastReadMessageID: targetMemberPayload.LastReadMessageID,
				JoinedAt:          targetMemberPayload.JoinedAt,
			}
			_ = cache_service.UpdateMemberSpeedCache(ctx, liteMemberRequest)

			// 2. PERSISTANCE BATCH (Write-Behind)
			errQueue := redis.EnqueueDB(ctx, targetMemberPayload.ID, conversationID, redis.EntityMembers, redis.ActionUpdate, targetMemberPayload, redis.TargetAll)
			if errQueue != nil {
				numan_log.Error(ctx).Err(errQueue).Int64("member_id", targetMemberPayload.ID).Msg("Échec file d'attente pour l'intégration de membre en masse")
			}

			// 3. MESSAGES SYSTÈMES ET DIFFUSION WEBSOCKET (Mode Twitch Communauté)
			if targetUserLite, errLite := cache_service.GetUserLite(ctx, targetMemberPayload.UserID); errLite == nil {

				callerUserLite, errCaller := cache_service.GetUserLite(ctx, callerAdminID)
				callerUsername := "Unknown User"
				if errCaller == nil {
					callerUsername = callerUserLite.Username
				}
				targetUsername := "Unknown User"
				if errLite == nil {
					targetUsername = targetUserLite.Username
				}

				systemMessageInput := message_models.CreateMessageInput{
					MessageType: variables.MessageTypeSystem,
					Content:     "",
					Attachments: map[string]any{
						"sys_action": variables.SysActionMemberJoinedThanksTo,
						"actor": map[string]any{
							"id":       callerAdminID,
							"username": callerUsername,
						},
						"target": map[string]any{
							"id":       targetMemberPayload.UserID,
							"username": targetUsername,
						},
					},
				}
				// Expédition du message de bienvenue système
				_, _ = message_service.CreateMessage(ctx, callerAdminID, conversationID, systemMessageInput, true)

				memberViewDto := member_models.MemberView{
					MemberPayload:     targetMemberPayload,
					Username:          targetUserLite.Username,
					IsOnline:          cache_service.IsUserOnline(ctx, targetMemberPayload.UserID),
					AvatarCommunityID: targetUserLite.ProfilePictureID, // Mode Twitch (Idéal pour les grosses communautés)
				}

				_ = realtime_service.BroadcastToConversation(ctx, conversationID, "member.joined", memberViewDto)
			}
		}
	}
}

// fetchCandidates récupère les candidats bruts en L1 (Recherche ou Amis).
func fetchCandidates(ctx context.Context, callerID int64, query string, offset int64, limit int64) []lite_models.UserLiteRequest {
	var candidates []lite_models.UserLiteRequest
	var errCache error

	if query == "" {
		candidates, errCache = cache_service.GetAddableUsersFromSpeedCache(ctx, callerID, limit, offset, false)
		if errCache != nil {
			numan_log.Warn(ctx).Err(errCache).Msg("Erreur lors de la récupération des AddableUsers en RAM")
		}
	} else {
		candidates, errCache = cache_service.SearchUserByPrefix(ctx, query, offset, limit)
		if errCache != nil {
			numan_log.Warn(ctx).Err(errCache).Msg("Erreur lors de la recherche par préfixe en RAM")
		}
	}
	return candidates
}

// isCommunicationAllowed vérifie si l'intention correspond aux paramètres de confidentialité de la cible.
func isCommunicationAllowed(intentAction string, relationState int, targetUserLite lite_models.UserLiteRequest) bool {
	if relationState == variables.RelationStateBlocked {
		return false
	}

	switch intentAction {
	case "group":
		return targetUserLite.AddGroupPermission == 0 || (targetUserLite.AddGroupPermission == 1 && relationState == 2)
	case "dm":
		return targetUserLite.ConversationPermission == 0 ||
			(targetUserLite.ConversationPermission == 1 && relationState >= 1) ||
			(targetUserLite.ConversationPermission == 2 && relationState == 2)
	case "tag":
		return targetUserLite.AllowTagging == 0 ||
			(targetUserLite.AllowTagging == 1 && relationState >= 1) ||
			(targetUserLite.AllowTagging == 2 && relationState == 2)
	case "mention":
		return targetUserLite.AllowMentions == 0 ||
			(targetUserLite.AllowMentions == 1 && relationState >= 1) ||
			(targetUserLite.AllowMentions == 2 && relationState == 2)
	default:
		return true
	}
}
