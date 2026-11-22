package conversation_service

import (
	"context"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	infraPostgres "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	pgRepo "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR DES MÉTADONNÉES D'UNE CONVERSATION
// ############################################################################

// UpdateConversation gère la modification (Titre, Règles, Description, Avatar, Liens, Langue)
// d'un groupe ou d'une communauté. Rejette automatiquement les MP.
func UpdateConversation(ctx context.Context, callerID int64, conversationID int64, input conversation_models.UpdateConversationInput) (conversation_models.UpdateConversationOutput, error) {
	if callerID <= 0 {
		return conversation_models.UpdateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est invalide.", nil)
	}
	if conversationID <= 0 {
		return conversation_models.UpdateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if input.ConversationID == 0 {
		input.ConversationID = conversationID
	} else if input.ConversationID != conversationID {
		return conversation_models.UpdateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation ne correspond pas à la cible.", nil)
	}
	if err := input.Validate(); err != nil {
		return conversation_models.UpdateConversationOutput{}, err
	}

	// ── ÉTAPE 1 : CONTRÔLE DE SÉCURITÉ ET RÉCUPÉRATION COMPLÈTE ─────────────

	conversationPayload, errSecurity := security_service.LeftConversation(ctx, conversationID, callerID)
	if errSecurity != nil {
		return conversation_models.UpdateConversationOutput{}, errSecurity
	}

	if conversationPayload.Type == variables.ConversationTypeDirect {
		return conversation_models.UpdateConversationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Impossible de modifier les métadonnées ou les paramètres d'un message privé.", nil)
	}

	// ── ÉTAPE 2 : ÉVALUATION ET APPLICATION DES MODIFICATIONS ───────────────

	hasTitleChanged := false
	if input.Title != conversationPayload.Title {
		conversationPayload.Title = input.Title
		hasTitleChanged = true
	}

	hasDescriptionChanged := false
	hasAvatarChanged := false
	hasSettingsChanged := false
	hasLinkChanged := false
	hasLangChanged := false
	oldLang := conversationPayload.Lang
	oldPinSlot := conversationPayload.PinSlot

	// Évaluation spécifique de la règle d'approbation (Pour déclenchement de routine de fond)
	wasApprovalRequired := conversationPayload.Settings.JoinApprovalRequired

	if input.Settings != conversationPayload.Settings {
		conversationPayload.Settings = input.Settings
		hasSettingsChanged = true
	}

	isApprovalRestrictionDropped := conversationPayload.Type == variables.ConversationTypeCommunityPub && wasApprovalRequired && !conversationPayload.Settings.JoinApprovalRequired

	// Restrictions selon le type (Seules les communautés publiques ont le droit à tous les champs)
	if conversationPayload.Type == variables.ConversationTypeCommunityPub {
		if input.Description != conversationPayload.Description {
			conversationPayload.Description = input.Description
			hasDescriptionChanged = true
		}
		if input.AvatarID != conversationPayload.AvatarID {
			conversationPayload.AvatarID = input.AvatarID
			hasAvatarChanged = true
		}
		if input.ExternalLink != conversationPayload.ExternalLink {
			conversationPayload.ExternalLink = input.ExternalLink
			hasLinkChanged = true
		}
		if strings.TrimSpace(input.Lang) != "" {
			normalizedLang, okLang := variables.NormalizeCommunityLang(input.Lang)
			if !okLang {
				return conversation_models.UpdateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue spécifiée n'est pas supportée.", nil)
			}
			if normalizedLang != conversationPayload.Lang {
				conversationPayload.Lang = normalizedLang
				conversationPayload.PinSlot = 0
				hasLangChanged = true
			}
		}
	} else {
		// Tolérance zéro si un groupe standard essaie de modifier des données de communauté
		if (input.Description != "" && input.Description != conversationPayload.Description) ||
			(input.AvatarID != 0 && input.AvatarID != conversationPayload.AvatarID) ||
			(input.ExternalLink.URL != "" || input.ExternalLink.Title != "") ||
			strings.TrimSpace(input.Lang) != "" {
			return conversation_models.UpdateConversationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seules les communautés publiques peuvent posséder une description, un avatar, une langue ou un lien externe.", nil)
		}
	}

	conversationPayload.UpdatedAt = domain.NowMillis()

	if err := conversationPayload.Validate(); err != nil {
		return conversation_models.UpdateConversationOutput{}, err
	}

	// ── ÉTAPE 2B : SYNCHRONISATION L3 ATOMIQUE EN CAS DE CHANGEMENT DE LANGUE ─
	if hasLangChanged && infraPostgres.PostgresDB != nil {
		if _, errPgLang := pgRepo.FuncUpdateCommunityLang(ctx, conversationPayload.ID, conversationPayload.Lang); errPgLang != nil {
			numan_log.Error(ctx).Err(errPgLang).Int64("conv_id", conversationPayload.ID).Str("old_lang", oldLang).Str("new_lang", conversationPayload.Lang).Msg("Échec L3 de mise à jour atomique de la langue de communauté")
			return conversation_models.UpdateConversationOutput{}, errPgLang
		}
	}

	// ── ÉTAPE 3 : MISE À JOUR IMMÉDIATE L1 (RAM OBJECT & SPEED CACHE) ───────

	_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)

	if hasTitleChanged || hasDescriptionChanged || hasAvatarChanged || hasSettingsChanged || hasLinkChanged || hasLangChanged {
		liteConversationRequest := service.ToConvLiteRequest(conversationPayload)
		_ = redis.ConvMeta.SetObject(ctx, conversationPayload.ID, liteConversationRequest)
	}

	// ── ÉTAPE 3B : RÉCONCILIATION REDIS RECOMMANDATION SUR CHANGEMENT DE LANGUE
	if hasLangChanged {
		if oldLang != "" {
			if oldPinSlot > 0 {
				if errDelPin := cache_service.DeleteCommunityPinInCache(ctx, oldLang, oldPinSlot); errDelPin != nil {
					numan_log.Warn(ctx).Err(errDelPin).Int64("conv_id", conversationPayload.ID).Str("old_lang", oldLang).Int("old_pin_slot", oldPinSlot).Msg("Échec de suppression du pin Redis dans l'ancienne langue")
				}
			}
			if errRemPin := cache_service.RemoveCommunityPinByConversationID(ctx, oldLang, conversationPayload.ID); errRemPin != nil {
				numan_log.Warn(ctx).Err(errRemPin).Int64("conv_id", conversationPayload.ID).Str("old_lang", oldLang).Msg("Échec de nettoyage résiduel du pin Redis dans l'ancienne langue")
			}
		}

		scoreToMove := 0.0
		hadScore := false
		if oldLang != "" {
			if prevScore, found, errGet := cache_service.GetRankedCommunityScore(ctx, oldLang, conversationPayload.ID); errGet == nil && found {
				scoreToMove = prevScore
				hadScore = true
			}
			if errRemRank := cache_service.RemoveRankedCommunity(ctx, oldLang, conversationPayload.ID); errRemRank != nil {
				numan_log.Warn(ctx).Err(errRemRank).Int64("conv_id", conversationPayload.ID).Str("old_lang", oldLang).Msg("Échec de retrait de la communauté de l'ancien ZSET de recommandation")
			}
		}

		if !hadScore {
			memberCount := 1
			if card, errCard := redis.ConvParticipants.SCard(ctx, conversationPayload.ID); errCard == nil && card > 0 {
				memberCount = int(card)
			}
			scoreToMove = community_recommendation_service.ComputeInitialCreationScore(
				models.ResolveCommunityModeFromWritePermission(conversationPayload.Settings.WritePermission),
				conversationPayload.IsPartner,
				false,
				0,
				memberCount,
				domain.NowTime(),
				community_recommendation_service.LoadScoringConfig(),
			)
		}

		if errUpsert := cache_service.UpsertRankedCommunityScore(ctx, conversationPayload.Lang, conversationPayload.ID, scoreToMove); errUpsert != nil {
			numan_log.Warn(ctx).Err(errUpsert).Int64("conv_id", conversationPayload.ID).Str("new_lang", conversationPayload.Lang).Msg("Échec de migration de la communauté vers le nouveau ZSET de recommandation")
		}
	}

	// ── ÉTAPE 4 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	errQueue := redis.EnqueueDB(ctx, conversationPayload.ID, conversationPayload.ID, redis.EntityConversation, redis.ActionUpdate, conversationPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("conv_id", conversationPayload.ID).Msg("Échec du Write-Behind lors de la modification de la conversation")
		return conversation_models.UpdateConversationOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : DÉLÉGATION WEBSOCKETS ET ROUTINES DE FOND ─────────────────

	go func(payload conversation_models.ConversationPayload, convID int64, adminID int64, dropApproval bool) {
		backgroundContext := context.Background()

		// A. Émission Temps Réel (WebSockets)
		errBroadcast := realtime_service.BroadcastToConversation(backgroundContext, convID, "conversation.updated", payload)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Erreur d'émission WebSocket pour conversation.updated")
		}

		// B. SYNC LEDGER (Trigger d'Invalition Mutuelle Global)
		participantsStringList, _ := redis.ConvParticipants.SMembers(backgroundContext, convID)
		syncTargetIDs := pkg.ParseInt64List(participantsStringList)
		_ = cache_service.RecordConversationMutation(backgroundContext, convID, syncTargetIDs)

		// C. Traitement massif des membres si la communauté vient d'être passée en "Entrée Libre"
		if dropApproval {
			acceptAllPendingMembers(backgroundContext, convID, adminID)
		}
	}(conversationPayload, conversationPayload.ID, callerID, isApprovalRestrictionDropped)

	// ── ÉTAPE 6 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return conversation_models.UpdateConversationOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
