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
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
)

// ############################################################################
// # SERVICE : MISE À JOUR DES PARAMÈTRES PERSONNELS DU MEMBRE
// ############################################################################

// UpdateMemberSettings gère la mise à jour partielle des paramètres de silence et de médias
// d'un membre pour une conversation spécifique.
func UpdateMemberSettings(ctx context.Context, callerID int64, input member_models.UpdateMemberSettingsInput) (member_models.UpdateMemberSettingsOutput, error) {

	// ── ÉTAPE 1 : RÉCUPÉRATION ET CONTRÔLE D'ACCÈS ──────────────────────────

	memberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.UpdateMemberSettingsOutput{}, errSecurity
	}

	// ── ÉTAPE 2 : MODIFICATION PARTIELLE ────────────────────────────────────

	memberPayload.Settings.IsMuted = input.IsMuted
	memberPayload.Settings.MuteExpireAt = input.MuteExpiresAt
	memberPayload.Settings.MediaAutoDownload = input.MediaAutoDownload
	memberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 3 : MISE À JOUR SYNCHRONE DU CACHE RAM L1 ─────────────────────

	_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)

	liteMemberRequest := lite_models.MemberLiteRequest{
		ConversationID:    memberPayload.ConversationID,
		UserID:            memberPayload.UserID,
		Role:              memberPayload.Role,
		Settings:          service.ToMemberSettingsLite(memberPayload.Settings),
		UnreadCount:       memberPayload.UnreadCount,
		FrozenMessageID:   memberPayload.FrozenMessageID,
		LastReadMessageID: memberPayload.LastReadMessageID,
		JoinedAt:          memberPayload.JoinedAt,
	}
	_ = cache_service.UpdateMemberSpeedCache(ctx, liteMemberRequest)

	// ── ÉTAPE 4 : PERSISTANCE ASYNCHRONE ────────────────────────────────────

	errQueue := redis.EnqueueDB(ctx, memberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, memberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("conv_id", input.ConversationID).Msg("Échec du Write-Behind pour la mise à jour des paramètres du membre")
		return member_models.UpdateMemberSettingsOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : SYNC LEDGER (TRIGGER D'INVALIDATION LOCAL) ────────────────

	go func(cID int64, uID int64) {
		bgCtx := context.Background()
		// Le changement de paramètre (ex: Mute) n'affecte QUE l'utilisateur appelant,
		// on ne déclenche donc le signal de mutation que pour lui.
		_ = cache_service.RecordConversationMutation(bgCtx, cID, []int64{uID})
	}(input.ConversationID, callerID)

	// ── ÉTAPE 6 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.UpdateMemberSettingsOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
