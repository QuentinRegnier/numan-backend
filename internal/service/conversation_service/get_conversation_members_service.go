package conversation_service

import (
	"context"
	"sort"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
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
// # SERVICE : RÉCUPÉRATION DU ROSTER DES MEMBRES PAR CONVERSATION
// ############################################################################

// GetConversationMembers extrait les membres d'une conversation avec pagination,
// vérifie les droits d'accès et hydrate les profils avec auto-guérison.
func GetConversationMembers(ctx context.Context, callerID int64, input member_models.GetConversationMembersInput) (member_models.GetConversationMembersOutput, error) {
	if callerID <= 0 {
		return member_models.GetConversationMembersOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return member_models.GetConversationMembersOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// ── ÉTAPE 1 : VÉRIFICATION D'APPARTENANCE (SÉCURITÉ) ──────────────────
	if _, errMembership := security_service.LeftMember(ctx, input.ConversationID, callerID); errMembership != nil {
		return member_models.GetConversationMembersOutput{}, errMembership
	}

	// ── ÉTAPE 2 : IDENTIFICATION DU TYPE DE CONVERSATION ───────────────────
	conversationPayload, errConv := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)
	if errConv != nil || conversationPayload.ID == 0 {
		conversationPayload, _ = mongo.MongoGetConversation(ctx, input.ConversationID)
		if conversationPayload.ID == 0 {
			var errPg error
			conversationPayload, errPg = postgres.FuncGetConversation(ctx, input.ConversationID)
			if errPg != nil || conversationPayload.ID == 0 {
				numan_log.Warn(ctx).Err(errPg).Int64("conv_id", input.ConversationID).Msg("Échec fallback Postgres pour conversation")
				return member_models.GetConversationMembersOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable.", errPg)
			}
		}
	}

	// ── ÉTAPE 3 : RÉCUPÉRATION ET PAGINATION DES IDENTIFIANTS EN RAM (L1) ──
	var allParticipantIDs []int64
	cachedParticipantsList, errRedisMembers := redis.ConvParticipants.SMembers(ctx, input.ConversationID)

	if errRedisMembers == nil && len(cachedParticipantsList) > 0 {
		allParticipantIDs = pkg.ParseInt64List(cachedParticipantsList)
	} else {
		var errPgParticipants error
		allParticipantIDs, errPgParticipants = postgres.FuncGetConversationParticipantIDs(ctx, input.ConversationID)
		if errPgParticipants != nil {
			numan_log.Error(ctx).Err(errPgParticipants).Int64("conv_id", input.ConversationID).Msg("Erreur L3 lors du chargement des participants")
			return member_models.GetConversationMembersOutput{}, numan_error.NewInternal(errPgParticipants)
		}
		// Auto-guérison L1 du set de distribution
		for _, pID := range allParticipantIDs {
			_ = redis.ConvParticipants.SAdd(ctx, input.ConversationID, pID)
		}
	}

	// Tri (Sort) indispensable car un Set Redis n'est pas ordonné.
	// Sans ça, la pagination (Offset/Limit) serait chaotique entre deux appels du client.
	sort.Slice(allParticipantIDs, func(i, j int) bool {
		return allParticipantIDs[i] < allParticipantIDs[j]
	})

	// Application de la pagination (Slice bounds en RAM - Exécution ultra-rapide)
	totalMembers := int64(len(allParticipantIDs))
	if input.Offset >= totalMembers {
		return member_models.GetConversationMembersOutput{
			ConversationID: input.ConversationID,
			Members:        []member_models.MemberView{}, // Fin de liste
		}, nil
	}

	endIndex := input.Offset + input.Limit
	if endIndex > totalMembers {
		endIndex = totalMembers
	}
	paginatedParticipantIDs := allParticipantIDs[input.Offset:endIndex]

	// ── ÉTAPE 4 : HYDRATATION DU SOUS-ENSEMBLE PAGINÉ ───────────────────
	hydratedMembersView := make([]member_models.MemberView, 0, len(paginatedParticipantIDs))

	for _, participantUserID := range paginatedParticipantIDs {
		memberPayload, errCacheMember := object_cache_service.GetMemberFromObjectCache(ctx, input.ConversationID, participantUserID)

		if errCacheMember != nil || memberPayload.ID == 0 {
			var errMongo error
			memberPayload, errMongo = mongo.MongoGetMember(ctx, input.ConversationID, participantUserID)
			if errMongo == nil && memberPayload.ID != 0 {
				go func(m member_models.MemberPayload) {
					_ = object_cache_service.SetMemberInObjectCache(context.Background(), m)
				}(memberPayload)
			} else {
				var errPg error
				memberPayload, errPg = postgres.FuncGetMember(ctx, input.ConversationID, participantUserID)
				if errPg != nil {
					numan_log.Warn(ctx).Err(errPg).Int64("user_id", participantUserID).Msg("Échec fallback Postgres pour membre")
					continue
				}
				if memberPayload.ID != 0 {
					go func(m member_models.MemberPayload) {
						bgContext := context.Background()
						_ = object_cache_service.SetMemberInObjectCache(bgContext, m)
						_ = redis.EnqueueDB(bgContext, m.ID, m.ConversationID, redis.EntityMembers, redis.ActionUpdate, m, redis.TargetMongo)
					}(memberPayload)
				}
			}
		}

		// Exclusion des participants ayant quitté ou ayant été bannis
		if memberPayload.ID == 0 || memberPayload.Role < variables.MemberRoleNormal {
			continue
		}

		memberView := member_models.MemberView{
			MemberPayload: memberPayload,
			IsOnline:      cache_service.IsUserOnline(ctx, participantUserID),
		}

		if userLiteData, errLite := cache_service.GetUserLite(ctx, participantUserID); errLite == nil {
			memberView.Username = userLiteData.Username

			if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
				memberView.AvatarCommunityID = userLiteData.ProfilePictureID
			} else if userLiteData.ProfilePictureID > 0 {
				if avatarView, errMedia := media_service.GenerateMediaViewCascade(ctx, userLiteData.ProfilePictureID, participantUserID, 0, callerID); errMedia == nil {
					memberView.Avatar = avatarView
				}
			}
		}

		hydratedMembersView = append(hydratedMembersView, memberView)
	}

	return member_models.GetConversationMembersOutput{
		ConversationID: input.ConversationID,
		Members:        hydratedMembersView,
	}, nil
}
