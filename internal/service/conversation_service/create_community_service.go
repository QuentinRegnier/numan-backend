package conversation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : CRÉATION D'UNE COMMUNAUTÉ PUBLIQUE (TYPE 3)
// ############################################################################

// CreateCommunity valide les prérequis de réputation, instancie la communauté
// et initialise ses structures de recherche et de Speed Cache.
func CreateCommunity(ctx context.Context, callerID int64, input conversation_models.CreateCommunityInput) (conversation_models.CreateCommunityOutput, error) {
	if callerID <= 0 {
		return conversation_models.CreateCommunityOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if err := input.Validate(); err != nil {
		return conversation_models.CreateCommunityOutput{}, err
	}

	normalizedLang, okLang := variables.NormalizeCommunityLang(input.Lang)
	if !okLang {
		return conversation_models.CreateCommunityOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue spécifiée n'est pas supportée.", nil)
	}

	// ── ÉTAPE 1 : CONTRÔLE DE RANG ET DE QUOTA ──────────────────────────────
	callerLite, errCaller := cache_service.GetUserLite(ctx, callerID)
	if errCaller != nil || callerLite.ID == 0 {
		return conversation_models.CreateCommunityOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Profil demandeur introuvable.", errCaller)
	}

	if callerLite.Grade < variables.CommunityMinCreationGrade {
		return conversation_models.CreateCommunityOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'avez pas le grade requis pour créer une communauté publique.", nil)
	}

	// Plafond pour les collaborateurs (Grade 2)
	if callerLite.Grade == variables.CommunityMinCreationGrade {
		inboxItems, errInbox := cache_service.GetInboxView(ctx, callerID, 1000, 0)
		if errInbox == nil {
			activeOwnedCount := 0
			for _, item := range inboxItems {
				if item.Conversation.Type == variables.ConversationTypeCommunityPub && item.Member.Role == variables.MemberRoleOwner {
					activeOwnedCount++
				}
			}
			if activeOwnedCount >= variables.MaxPublicCommunitiesColl {
				return conversation_models.CreateCommunityOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous gérez déjà une communauté publique. Une validation est nécessaire pour en créer d'autres.", nil)
			}
		}
	}

	// ── ÉTAPE 2 : DÉFINITION DU PROPRIÉTAIRE INITIAL ────────────────────────
	designatedOwnerID := callerID
	ownerTrustScore := callerLite.TrustScore

	if input.OwnerID != 0 && input.OwnerID != callerID {
		if callerLite.Grade >= variables.UserGradeModerator {
			targetOwnerLite, errTarget := cache_service.GetUserLite(ctx, input.OwnerID)
			if errTarget != nil || targetOwnerLite.ID == 0 {
				return conversation_models.CreateCommunityOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'utilisateur désigné comme propriétaire n'existe pas.", errTarget)
			}
			designatedOwnerID = input.OwnerID
			ownerTrustScore = targetOwnerLite.TrustScore
		} else {
			return conversation_models.CreateCommunityOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les modérateurs et administrateurs peuvent attribuer la propriété à un tiers.", nil)
		}
	}

	// ── ÉTAPE 3 : INITIALISATION DES PAYLOADS MÉTIER ────────────────────────
	currentTime := domain.NowTime()
	newConversationID := pkg.GenerateID()

	conversationSettings := input.Settings
	if conversationSettings == (conversation_models.ConversationSettings{}) {
		conversationSettings = conversation_models.DefaultConversationSettings(variables.ConversationTypeCommunityPub)
	}

	communityPayload := conversation_models.ConversationPayload{
		ID:            newConversationID,
		Type:          variables.ConversationTypeCommunityPub,
		Title:         pkg.CleanStr(input.Title),
		Description:   "",
		AvatarID:      0,
		LastMessageID: 0,
		State:         0,
		Settings:      conversationSettings,
		ExternalLink:  input.ExternalLink,
		Lang:          normalizedLang,
		CreatedAt:     domain.TimeToMillis(currentTime),
		UpdatedAt:     domain.TimeToMillis(currentTime),
	}

	ownerMemberPayload := member_models.MemberPayload{
		ID:                pkg.GenerateID(),
		ConversationID:    newConversationID,
		UserID:            designatedOwnerID,
		Role:              variables.MemberRoleOwner,
		Settings:          member_models.DefaultMemberSettings(communityPayload.Type),
		JoinedAt:          domain.TimeToMillis(currentTime),
		UnreadCount:       0,
		FrozenMessageID:   0,
		LastReadMessageID: 0,
		CreatedAt:         domain.TimeToMillis(currentTime),
		UpdatedAt:         domain.TimeToMillis(currentTime),
	}

	var adminCreatorPayload *member_models.MemberPayload
	if callerID != designatedOwnerID {
		adminCreatorPayload = &member_models.MemberPayload{
			ID:                pkg.GenerateID(),
			ConversationID:    newConversationID,
			UserID:            callerID,
			Role:              variables.MemberRoleNormal,
			Settings:          member_models.DefaultMemberSettings(communityPayload.Type),
			JoinedAt:          domain.TimeToMillis(currentTime),
			UnreadCount:       0,
			FrozenMessageID:   0,
			LastReadMessageID: 0,
			CreatedAt:         domain.TimeToMillis(currentTime),
			UpdatedAt:         domain.TimeToMillis(currentTime),
		}
	}

	if err := communityPayload.Validate(); err != nil {
		return conversation_models.CreateCommunityOutput{}, err
	}
	if err := ownerMemberPayload.Validate(); err != nil {
		return conversation_models.CreateCommunityOutput{}, err
	}
	if adminCreatorPayload != nil {
		if err := adminCreatorPayload.Validate(); err != nil {
			return conversation_models.CreateCommunityOutput{}, err
		}
	}

	// ── ÉTAPE 4 : INDEXATION EN CACHE L1 ET SPEED CACHE ─────────────────────
	_ = object_cache_service.SetConversationInObjectCache(ctx, communityPayload)
	_ = object_cache_service.SetMemberInObjectCache(ctx, ownerMemberPayload)
	cache_service.RehydrateConversationItemInSpeedCache(ctx, communityPayload, ownerMemberPayload, 0)

	initialMembers := 1
	if adminCreatorPayload != nil {
		initialMembers = 2
		_ = object_cache_service.SetMemberInObjectCache(ctx, *adminCreatorPayload)
		cache_service.RehydrateConversationItemInSpeedCache(ctx, communityPayload, *adminCreatorPayload, 0)
	}

	communitySearchRecord := lite_models.CommunityLiteRequest{
		ID:               newConversationID,
		Name:             input.Title,
		ProfilePictureID: 0,
		Description:      "",
		MemberCount:      initialMembers,
	}
	_ = cache_service.StoreCommunityLiteInSpeedCache(ctx, communitySearchRecord)

	// ── ÉTAPE 4B : INDEXATION RECOMMANDATION COMMUNAUTÉS PUBLIQUES (L1) ─────
	if _, errIdx := cache_service.AddUserPublicCommunity(ctx, designatedOwnerID, newConversationID); errIdx != nil {
		numan_log.Warn(ctx).Err(errIdx).Int64("conv_id", newConversationID).Int64("user_id", designatedOwnerID).Msg("Échec d'ajout du propriétaire dans user:public_comms")
	}
	if adminCreatorPayload != nil {
		if _, errIdx := cache_service.AddUserPublicCommunity(ctx, callerID, newConversationID); errIdx != nil {
			numan_log.Warn(ctx).Err(errIdx).Int64("conv_id", newConversationID).Int64("user_id", callerID).Msg("Échec d'ajout du créateur dans user:public_comms")
		}
	}

	initialScore := community_recommendation_service.ComputeInitialCreationScore(
		models.ResolveCommunityModeFromWritePermission(communityPayload.Settings.WritePermission),
		communityPayload.IsPartner,
		ownerTrustScore > 0,
		ownerTrustScore,
		initialMembers,
		currentTime,
		community_recommendation_service.LoadScoringConfig(),
	)
	if errRank := cache_service.UpsertRankedCommunityScore(ctx, normalizedLang, newConversationID, initialScore); errRank != nil {
		numan_log.Warn(ctx).Err(errRank).Int64("conv_id", newConversationID).Str("lang", normalizedLang).Msg("Échec d'initialisation du score dans le ZSET de recommandation")
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE WRITE-BEHIND ───────────────────────
	if errQueue := redis.EnqueueDB(ctx, communityPayload.ID, communityPayload.ID, redis.EntityConversation, redis.ActionCreate, communityPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("conv_id", communityPayload.ID).Msg("Échec d'enqueue de la communauté")
	}

	if errQueue := redis.EnqueueDB(ctx, ownerMemberPayload.ID, communityPayload.ID, redis.EntityMembers, redis.ActionCreate, ownerMemberPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("member_id", ownerMemberPayload.ID).Msg("Échec d'enqueue du propriétaire de la communauté")
	}

	if adminCreatorPayload != nil {
		_ = redis.EnqueueDB(ctx, adminCreatorPayload.ID, communityPayload.ID, redis.EntityMembers, redis.ActionCreate, *adminCreatorPayload, redis.TargetAll)
	}

	// ── ÉTAPE 6 : MARQUAGE D'ACTIVITÉ ───────────────────────────────────────
	timestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return conversation_models.CreateCommunityOutput{
		ConversationID: newConversationID,
		InboxUpdateAt:  timestampMs,
	}, nil
}
