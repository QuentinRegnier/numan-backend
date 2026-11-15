package member_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : LEVÉE DE BANNISSEMENT (UNBAN EN MASSE)
// ############################################################################

// UnbanMembers annule le bannissement d'un lot d'utilisateurs.
// L'action les passe au statut "A Quitté" (-1), leur permettant de postuler à nouveau.
func UnbanMembers(ctx context.Context, callerID int64, input member_models.UnbanMembersInput) (member_models.UnbanMembersOutput, error) {
	if callerID <= 0 {
		return member_models.UnbanMembersOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return member_models.UnbanMembersOutput{}, err
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ────────────────────────────────

	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.UnbanMembersOutput{}, errSecurity
	}
	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.UnbanMembersOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs peuvent débannir des utilisateurs.", nil)
	}

	currentTimeMs := domain.NowMillis()

	// ── ÉTAPE 2 : TRAITEMENT EN LOTS (BATCH UNBAN) ──────────────────────────

	for _, targetUserID := range input.TargetUserIDs {

		// Extraction du membre banni (Cascade L1 -> L2 -> L3 traitée par LeftMember)
		targetMemberPayload, errFetchTarget := security_service.LeftMember(ctx, input.ConversationID, targetUserID)
		if errFetchTarget != nil {
			continue // S'il n'existe pas, on ignore silencieusement
		}

		if targetMemberPayload.Role == variables.MemberRoleBanned {

			// ── ÉTAPE 3 : APPLICATION DU PARDON ─────────────────────────────

			// On modifie le rôle à -1 (A Quitté). L'utilisateur n'est plus banni
			// et pourra rejoindre librement la communauté à l'avenir.
			targetMemberPayload.Role = variables.MemberRoleLeft
			targetMemberPayload.UpdatedAt = currentTimeMs
			targetMemberPayload.FrozenMessageID = 0 // Suppression de la restriction d'historique

			// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE RAM L1 ──────────────────────

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

			// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE ────────────────────────────

			errQueue := redis.EnqueueDB(ctx, targetMemberPayload.ID, targetMemberPayload.ConversationID, redis.EntityMembers, redis.ActionUpdate, targetMemberPayload, redis.TargetAll)
			if errQueue != nil {
				numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetUserID).Msg("Échec du Write-Behind lors du débannissement")
			}
		}
	}

	// ── ÉTAPE 6 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.UnbanMembersOutput{
		InboxUpdateAt: latestActivityTimestampMs, // Modifié ici, il ne faut plus utiliser `TimeToMillis` si `TouchInboxActivity` renvoie déjà les ms
	}, nil
}
