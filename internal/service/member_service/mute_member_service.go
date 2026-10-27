package member_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RESTRICTION TEMPORELLE (MUTE) D'UN MEMBRE
// ############################################################################

// MuteMember gère l'interdiction de parler pour un membre.
func MuteMember(ctx context.Context, callerID int64, input member_models.MuteMemberInput) error {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ET HIERARCHIE ────────────────────────────

	callerMemberPayload, errCaller := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errCaller != nil {
		return errCaller
	}
	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs peuvent appliquer une restriction à un membre.", nil)
	}

	targetMemberPayload, errTarget := security_service.LeftMember(ctx, input.ConversationID, input.TargetUserID)
	if errTarget != nil {
		return errTarget
	}

	// Un administrateur ne peut pas muter un autre administrateur ou un propriétaire.
	if targetMemberPayload.Role >= callerMemberPayload.Role {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Vous ne pouvez pas restreindre un membre de rang égal ou supérieur au vôtre.", nil)
	}

	// ── ÉTAPE 2 : LOGIQUE ALGORITHMIQUE ET CASCADE (L1 -> L2 -> L3) ─────────
	// Objectif : Ne pas muter quelqu'un qui n'a déjà pas le droit de parler selon les lois du groupe.

	var conversationPayload conversation_models.ConversationPayload

	conversationPayload, errCache := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)

	if errCache != nil || conversationPayload.ID == 0 {
		var errMongo error
		conversationPayload, errMongo = mongo.MongoGetConversation(ctx, input.ConversationID)

		if errMongo == nil && conversationPayload.ID != 0 {
			// AUTO-GUÉRISON L2 -> L1
			_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)
		} else {
			// FALLBACK L3 (PostgreSQL)
			var errPg error
			conversationPayload, errPg = postgres.FuncGetConversation(ctx, input.ConversationID)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("conv_id", input.ConversationID).Msg("Échec de la récupération L3 de la conversation pour le Mute")
				return numan_error.NewInternal()
			}

			if conversationPayload.ID != 0 {
				// AUTO-GUÉRISON L3 -> L2 & L1
				go func(c conversation_models.ConversationPayload) {
					bgCtx := context.Background()
					_ = redis.EnqueueDB(bgCtx, c.ID, c.ID, redis.EntityConversation, redis.ActionUpdate, c, redis.TargetMongo)
				}(conversationPayload)

				_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)
			}
		}
	}

	if conversationPayload.ID == 0 {
		return numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable ou inactive.", nil)
	}

	// Règle métier : Si le groupe est en mode "Admins Uniquement" (WritePermission = 1),
	// le membre standard n'a déjà pas le droit de parole, la sanction est inutile.
	if conversationPayload.Settings.WritePermission == 1 && targetMemberPayload.Role == variables.MemberRoleNormal {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Ce membre n'a déjà pas le droit de parole en raison des permissions actuelles du groupe.", nil)
	}

	// ── ÉTAPE 3 : APPLICATION DE LA PUNITION ────────────────────────────────

	targetMemberPayload.Settings.RestrictedUntil = input.RestrictedUntil
	targetMemberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE (L1 OBJECT ET SPEED CACHE) ──────────

	_ = object_cache_service.SetMemberInObjectCache(ctx, targetMemberPayload)

	memberCompositeID := fmt.Sprintf("%d:%d", input.ConversationID, input.TargetUserID)
	var liteMember lite_models.MemberLiteRequest

	if errSpeedCache := redis.ConvMembers.GetObject(ctx, memberCompositeID, &liteMember); errSpeedCache == nil {
		liteMember.Settings.RestrictedUntil = input.RestrictedUntil
		_ = redis.ConvMembers.SetObject(ctx, memberCompositeID, liteMember)
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE ET LEDGER (WRITE-BEHIND) ───────────

	errQueue := redis.EnqueueDB(ctx, targetMemberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, targetMemberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetMemberPayload.UserID).Msg("Échec du Write-Behind pour la restriction d'un membre")
		return numan_error.NewInternal()
	}

	// Malus Trust Score si exclusion d'une communauté publique (§C.13 : -0.10)
	if input.RestrictedUntil > domain.NowMillis() {
		_ = moderation_service.PublishTrustScoreEvent(
			ctx,
			input.TargetUserID,
			variables.TrustTriggerMutedInPublicComm,
			input.ConversationID,
			"Mute temporaire dans un salon ou communauté",
		)
	}

	// SYNC LEDGER (Trigger d'invalidation mutuelle)
	go func(cID int64) {
		bgCtx := context.Background()
		participantsStringList, _ := redis.ConvParticipants.SMembers(bgCtx, cID)

		syncTargetIDs := pkg.ParseInt64List(participantsStringList)
		_ = cache_service.RecordConversationMutation(bgCtx, cID, syncTargetIDs)
	}(input.ConversationID)

	// ── ÉTAPE 6 : NOTIFICATION ET MESSAGE SYSTÈME ───────────────────────────

	go func() {
		bgCtx := context.Background()

		// A. Diffusion temps réel de la pénalité pour l'interface UI
		_ = realtime_service.BroadcastToConversation(bgCtx, input.ConversationID, "member.muted", targetMemberPayload)

		// B. Message Système pour historique
		callerUserLite, errCaller := cache_service.GetUserLite(bgCtx, callerID)
		targetUserLite, errTarget := cache_service.GetUserLite(bgCtx, input.TargetUserID)

		callerUsername := "Unknown User"
		if errCaller == nil {
			callerUsername = callerUserLite.Username
		}
		targetUsername := "Unknown User"
		if errTarget == nil {
			targetUsername = targetUserLite.Username
		}

		systemMessageInput := message_models.CreateMessageInput{
			ConversationID: input.ConversationID,
			MessageType:    variables.MessageTypeSystem,
			Content:        "",
			Attachments: map[string]any{
				"sys_action": variables.SysActionMemberMuted,
				"actor": map[string]any{
					"id":       callerID,
					"username": callerUsername,
				},
				"target": map[string]any{
					"id":       input.TargetUserID,
					"username": targetUsername,
				},
				"metadata": map[string]any{
					"restricted_until": input.RestrictedUntil,
				},
			},
		}

		// By-pass du mode lecture-seule pour l'Admin s'il était lui-même restreint (isInternal = true)
		_, _ = message_service.CreateMessage(bgCtx, callerID, input.ConversationID, systemMessageInput, true)
	}()

	return nil
}
