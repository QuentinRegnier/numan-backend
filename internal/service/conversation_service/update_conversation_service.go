package conversation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR DES MÉTADONNÉES D'UNE CONVERSATION
// ############################################################################

// UpdateConversation gère la modification (Titre, Règles, Description, Avatar, Liens)
// d'un groupe ou d'une communauté. Rejette automatiquement les MP.
func UpdateConversation(ctx context.Context, callerID int64, conversationID int64, input conversation_models.UpdateConversationInput) (conversation_models.UpdateConversationOutput, error) {

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
	} else {
		// Tolérance zéro si un groupe standard essaie de modifier des données de communauté
		if (input.Description != "" && input.Description != conversationPayload.Description) ||
			(input.AvatarID != 0 && input.AvatarID != conversationPayload.AvatarID) ||
			(input.ExternalLink.URL != "" || input.ExternalLink.Title != "") {
			return conversation_models.UpdateConversationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seules les communautés publiques peuvent posséder une description, un avatar ou un lien externe.", nil)
		}
	}

	conversationPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 3 : MISE À JOUR IMMÉDIATE L1 (RAM OBJECT & SPEED CACHE) ───────

	_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)

	if hasTitleChanged || hasDescriptionChanged || hasAvatarChanged || hasSettingsChanged || hasLinkChanged {
		liteConversationRequest := lite_models.ConvLiteRequest{
			ID:            conversationPayload.ID,
			Type:          conversationPayload.Type,
			Title:         conversationPayload.Title,
			Description:   conversationPayload.Description,
			AvatarID:      conversationPayload.AvatarID,
			LastMessageID: conversationPayload.LastMessageID,
			Settings:      service.ToConversationSettingsLite(conversationPayload.Settings),
			ExternalLink: models.ExternalLinks{
				Title: conversationPayload.ExternalLink.Title,
				URL:   conversationPayload.ExternalLink.URL,
			},
		}
		_ = redis.ConvMeta.SetObject(ctx, conversationPayload.ID, liteConversationRequest)
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
