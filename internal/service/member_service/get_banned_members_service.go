package member_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : LISTE DES MEMBRES BANNIS
// ############################################################################

// GetBannedMembers retourne la liste paginée des utilisateurs bannis d'une conversation.
func GetBannedMembers(ctx context.Context, callerID int64, input member_models.GetBannedMembersInput) (member_models.GetBannedMembersOutput, error) {
	if callerID <= 0 {
		return member_models.GetBannedMembersOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return member_models.GetBannedMembersOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ────────────────────────────────
	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.GetBannedMembersOutput{}, errSecurity
	}

	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.GetBannedMembersOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs ont accès à la liste des utilisateurs bannis.", nil)
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DIRECTE DEPUIS POSTGRESQL (L3) ───────────────
	// Note architecturale : Les utilisateurs bannis ne sont pas conservés dans le cache volatil (L1)
	// pour économiser la RAM, on interroge donc directement le stockage à froid.
	bannedMembersPayloads, errPg := postgres.FuncLoadMembersByRolePaginated(ctx, input.ConversationID, variables.MemberRoleBanned, input.Limit, input.Offset)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Int64("conv_id", input.ConversationID).Msg("Échec L3 lors de la récupération de la liste des bannis")
		return member_models.GetBannedMembersOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 3 : HYDRATATION EN MASSE DES PROFILS ──────────────────────────
	var bannedUsersViews []member_models.BannedUserView

	for _, bannedMemberPayload := range bannedMembersPayloads {
		if bannedUserLite, errLite := cache_service.GetUserLite(ctx, bannedMemberPayload.UserID); errLite == nil {

			var userAvatarView media_models.MediaView
			if bannedUserLite.ProfilePictureID > 0 {
				if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, bannedUserLite.ProfilePictureID, bannedMemberPayload.UserID, 0, callerID); errMedia == nil {
					userAvatarView = mediaView
				}
			}

			bannedUsersViews = append(bannedUsersViews, member_models.BannedUserView{
				UserLiteView: auth_models.UserLiteView{
					User:     bannedUserLite,
					Avatar:   userAvatarView,
					IsOnline: cache_service.IsUserOnline(ctx, bannedMemberPayload.UserID),
				},
				BanDate: bannedMemberPayload.UpdatedAt, // L'UpdatedAt correspond à la date d'application de la sanction
			})
		}
	}

	// Prévention stricte du `null` en JSON pour les tableaux
	if bannedUsersViews == nil {
		bannedUsersViews = make([]member_models.BannedUserView, 0)
	}

	return member_models.GetBannedMembersOutput{
		BannedUsers: bannedUsersViews,
	}, nil
}
