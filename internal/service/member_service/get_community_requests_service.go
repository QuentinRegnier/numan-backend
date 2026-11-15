package member_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : LISTE DES DEMANDES D'ADHÉSION EN ATTENTE
// ############################################################################

// GetCommunityRequests récupère les candidatures en attente d'approbation.
func GetCommunityRequests(ctx context.Context, callerID int64, input member_models.GetCommunityRequestsInput) (member_models.GetCommunityRequestsOutput, error) {
	if callerID <= 0 {
		return member_models.GetCommunityRequestsOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return member_models.GetCommunityRequestsOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 20
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ────────────────────────────────

	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.GetCommunityRequestsOutput{}, errSecurity
	}

	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.GetCommunityRequestsOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous devez être administrateur pour consulter les demandes d'adhésion.", nil)
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DES MEMBRES EN ATTENTE (CASCADE L2 -> L3) ────

	var pendingMembersPayloads []member_models.MemberPayload

	// TENTATIVE L2 (Warm Storage MongoDB)
	membersFromMongo, errMongo := mongo.MongoLoadMembersByRolePaginated(input.ConversationID, variables.MemberRolePending, input.Limit, input.Offset)
	if errMongo == nil && len(membersFromMongo) > 0 {
		pendingMembersPayloads = membersFromMongo
	} else {
		// FALLBACK L3 (Cold Storage PostgreSQL)
		membersFromPostgres, errPg := postgres.FuncLoadMembersByRolePaginated(ctx, input.ConversationID, variables.MemberRolePending, input.Limit, input.Offset)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("conv_id", input.ConversationID).Msg("Échec L3 lors de la récupération des candidatures en attente")
			return member_models.GetCommunityRequestsOutput{}, numan_error.NewInternal()
		}

		pendingMembersPayloads = membersFromPostgres

		// AUTO-GUÉRISON L3 -> L2 (Asynchrone via Queue)
		for _, pendingMember := range pendingMembersPayloads {
			go func(m member_models.MemberPayload) {
				bgCtx := context.Background()
				_ = redis.EnqueueDB(bgCtx, m.ID, m.ConversationID, redis.EntityMembers, redis.ActionUpdate, m, redis.TargetMongo)
			}(pendingMember)
		}
	}

	// ── ÉTAPE 3 : HYDRATATION MASSIVE VIA SPEED CACHE (L1) ET GÉNÉRATION DES MÉDIAS

	var userRequestsViews []auth_models.UserLiteView

	for _, pendingMember := range pendingMembersPayloads {
		// Extraction en O(1) depuis la RAM (avec fallback local au service)
		if userLiteData, errLite := cache_service.GetUserLite(ctx, pendingMember.UserID); errLite == nil {

			var userAvatarView media_models.MediaView
			if userLiteData.ProfilePictureID > 0 {
				if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, userLiteData.ProfilePictureID, userLiteData.ID, 0, callerID); errMedia == nil {
					userAvatarView = mediaView
				}
			}

			userRequestsViews = append(userRequestsViews, auth_models.UserLiteView{
				User:     userLiteData,
				Avatar:   userAvatarView,
				IsOnline: cache_service.IsUserOnline(ctx, userLiteData.ID),
			})
		}
	}

	// Prévention stricte du `null` en JSON
	if userRequestsViews == nil {
		userRequestsViews = make([]auth_models.UserLiteView, 0)
	}

	return member_models.GetCommunityRequestsOutput{
		Requests: userRequestsViews,
	}, nil
}
