package security_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE DE SÉCURITÉ : DROITS D'ADMINISTRATION DE CONVERSATION
// ############################################################################

// LeftConversation vérifie les droits d'administration de l'utilisateur sur une conversation
// et retourne la conversation hydratée avec un mécanisme de cascade L1 -> L2 -> L3.
func LeftConversation(ctx context.Context, conversationID int64, userID int64) (conversation_models.ConversationPayload, error) {
	var conversationPayload conversation_models.ConversationPayload
	var memberPayload member_models.MemberPayload
	var isConversationFound, isMemberFound bool

	// ── ÉTAPE 1 : TENTATIVE L1 (OBJECT CACHE - LFU) ─────────────────────────

	if cachedConversation, err := object_cache_service.GetConversationFromObjectCache(ctx, conversationID); err == nil && cachedConversation.ID != 0 {
		conversationPayload = cachedConversation
		isConversationFound = true
	}

	if cachedMember, err := object_cache_service.GetMemberFromObjectCache(ctx, conversationID, userID); err == nil && cachedMember.ID != 0 {
		memberPayload = cachedMember
		isMemberFound = true
	}

	// ── ÉTAPE 2 : CASCADE L2 (MONGODB WARM STORAGE) ─────────────────────────

	if !isConversationFound {
		if mongoConversation, err := mongo.MongoGetConversation(ctx, conversationID); err == nil && mongoConversation.ID != 0 {
			conversationPayload = mongoConversation
			isConversationFound = true
			_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload) // Auto-Guérison L1
		}
	}

	if !isMemberFound {
		if mongoMember, err := mongo.MongoGetMember(ctx, conversationID, userID); err == nil && mongoMember.ID != 0 {
			memberPayload = mongoMember
			isMemberFound = true
			_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload) // Auto-Guérison L1
		}
	}

	// ── ÉTAPE 3 : CASCADE ABSOLUE L3 (POSTGRESQL COLD STORAGE) ──────────────

	if !isConversationFound {
		pgConversation, errPg := postgres.FuncGetConversation(ctx, conversationID)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("conv_id", conversationID).Msg("Erreur L3 lors de la récupération de la conversation")
			return conversation_models.ConversationPayload{}, errPg
		}

		if pgConversation.ID != 0 {
			conversationPayload = pgConversation
			isConversationFound = true

			// AUTO-GUÉRISON L1 (Immédiat en RAM)
			_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)

			// AUTO-GUÉRISON L2 (Asynchrone via Worker Mongo)
			go func(conv conversation_models.ConversationPayload) {
				backgroundCtx := context.Background()
				_ = redis.EnqueueDB(backgroundCtx, conv.ID, conv.ID, redis.EntityConversation, redis.ActionUpdate, conv, redis.TargetMongo)
			}(conversationPayload)
		}
	}

	if !isMemberFound {
		pgMember, errPg := postgres.FuncGetMember(ctx, conversationID, userID)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("user_id", userID).Msg("Erreur L3 lors de la récupération du membre")
			return conversation_models.ConversationPayload{}, errPg
		}

		if pgMember.ID != 0 {
			memberPayload = pgMember
			isMemberFound = true

			// AUTO-GUÉRISON L1 (Immédiat en RAM)
			_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)

			// AUTO-GUÉRISON L2 (Asynchrone via Worker Mongo)
			go func(mem member_models.MemberPayload) {
				backgroundCtx := context.Background()
				// PartitionKey = ConversationID pour grouper les requêtes membres d'une même conversation
				_ = redis.EnqueueDB(backgroundCtx, mem.ID, mem.ConversationID, redis.EntityMembers, redis.ActionUpdate, mem, redis.TargetMongo)
			}(memberPayload)
		}
	}

	// ── ÉTAPE 4 : VÉRIFICATION DES RÈGLES MÉTIER ET DE SÉCURITÉ ─────────────

	if !isConversationFound || conversationPayload.State == variables.ConversationStateArchived {
		return conversation_models.ConversationPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable ou archivée.", nil)
	}

	if !isMemberFound || memberPayload.Role < variables.MemberRoleNormal {
		return conversation_models.ConversationPayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous ne faites pas ou plus partie de cette conversation.", nil)
	}

	// Le rôle doit être au moins Admin (1) ou Propriétaire (2) pour satisfaire ce middleware
	if memberPayload.Role < variables.MemberRoleAdmin {
		return conversation_models.ConversationPayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : droits d'administration requis pour cette opération.", nil)
	}

	return conversationPayload, nil
}
