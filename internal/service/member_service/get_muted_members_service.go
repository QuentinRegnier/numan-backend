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
// # SERVICE : LISTE DES MEMBRES RESTREINTS (MUTE)
// ############################################################################

// GetMutedMembers récupère la liste des utilisateurs mutés (Interdiction d'écrire).
// Réservé aux administrateurs (1) et propriétaires (2) du groupe.
func GetMutedMembers(ctx context.Context, callerID int64, input member_models.GetMutedMembersInput) (member_models.GetMutedMembersOutput, error) {
	if callerID <= 0 {
		return member_models.GetMutedMembersOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return member_models.GetMutedMembersOutput{}, err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS (SÉCURITÉ ZERO-TRUST) ────────────────────

	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.GetMutedMembersOutput{}, errSecurity
	}
	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.GetMutedMembersOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs ont accès à cette liste.", nil)
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DEPUIS LA SOURCE DE VÉRITÉ (POSTGRESQL L3) ───

	// Note architecturale : Les membres mutés ne sont pas isolés dans le Speed Cache L1.
	// On interroge donc directement le stockage à froid pour filtrer sur "RestrictedUntil".
	mutedRecords, errPg := postgres.FuncLoadMutedMembersPaginated(ctx, input.ConversationID, input.Limit, input.Offset)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Int64("conv_id", input.ConversationID).Msg("Échec L3 lors de la récupération de la liste des membres mutés")
		return member_models.GetMutedMembersOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 3 : HYDRATATION EN MASSE VIA SPEED CACHE (L1) ─────────────────

	var mutedUserViews []member_models.MutedUserView

	for _, record := range mutedRecords {
		if userLiteData, errLite := cache_service.GetUserLite(ctx, record.UserID); errLite == nil {

			var userAvatarView media_models.MediaView
			if userLiteData.ProfilePictureID > 0 {
				if mediaView, errMedia := media_service.GenerateMediaViewCascade(ctx, userLiteData.ProfilePictureID, record.UserID, 0, callerID); errMedia == nil {
					userAvatarView = mediaView
				}
			}

			mutedUserViews = append(mutedUserViews, member_models.MutedUserView{
				UserLiteView: auth_models.UserLiteView{
					User:     userLiteData,
					Avatar:   userAvatarView,
					IsOnline: cache_service.IsUserOnline(ctx, record.UserID),
				},
				RestrictedUntil: record.RestrictedUntil,
			})
		}
	}

	// Prévention stricte du `null` en JSON
	if mutedUserViews == nil {
		mutedUserViews = make([]member_models.MutedUserView, 0)
	}

	return member_models.GetMutedMembersOutput{
		MutedUsers: mutedUserViews,
	}, nil
}
