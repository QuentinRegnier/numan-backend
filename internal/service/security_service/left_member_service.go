package security_service

import (
	"context"

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
// # SERVICE DE SÉCURITÉ : DROITS DE PARTICIPATION DU MEMBRE
// ############################################################################

// LeftMember vérifie que l'utilisateur fait partie de la conversation
// et retourne son profil de membre complet (L1 -> L2 -> L3) pour analyse.
func LeftMember(ctx context.Context, conversationID int64, userID int64) (member_models.MemberPayload, error) {
	var memberPayload member_models.MemberPayload
	var isMemberFound bool

	// ── ÉTAPE 1 : TENTATIVE L1 (OBJECT CACHE - LFU) ─────────────────────────

	if cachedMember, err := object_cache_service.GetMemberFromObjectCache(ctx, conversationID, userID); err == nil && cachedMember.ID != 0 {
		memberPayload = cachedMember
		isMemberFound = true
	}

	// ── ÉTAPE 2 : CASCADE L2 (MONGODB WARM STORAGE) ─────────────────────────

	if !isMemberFound {
		if mongoMember, err := mongo.MongoGetMember(ctx, conversationID, userID); err == nil && mongoMember.ID != 0 {
			memberPayload = mongoMember
			isMemberFound = true
			_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload) // Auto-Guérison L1
		}
	}

	// ── ÉTAPE 3 : CASCADE ABSOLUE L3 (POSTGRESQL COLD STORAGE) ──────────────

	if !isMemberFound {
		pgMember, errPg := postgres.FuncGetMember(ctx, conversationID, userID)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("user_id", userID).Msg("Erreur L3 lors de la vérification de l'appartenance d'un membre")
			return member_models.MemberPayload{}, errPg
		}

		if pgMember.ID != 0 {
			memberPayload = pgMember
			isMemberFound = true

			// AUTO-GUÉRISON L1 (Immédiat en RAM)
			_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)

			// AUTO-GUÉRISON L2 (Asynchrone via Worker Mongo)
			go func(mem member_models.MemberPayload) {
				backgroundCtx := context.Background()
				_ = redis.EnqueueDB(backgroundCtx, mem.ID, mem.ConversationID, redis.EntityMembers, redis.ActionUpdate, mem, redis.TargetMongo)
			}(memberPayload)
		}
	}

	// ── ÉTAPE 4 : VÉRIFICATION DES RÈGLES DE PARTICIPATION ──────────────────

	if !isMemberFound {
		return member_models.MemberPayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous n'êtes pas membre de cette conversation.", nil)
	}

	// Si le rôle est négatif (ex: Banni -2, Quitté -1, Rejeté -4)
	if memberPayload.Role < variables.MemberRoleNormal {
		return member_models.MemberPayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous êtes banni ou ne faites plus partie de cette conversation.", nil)
	}

	return memberPayload, nil
}
