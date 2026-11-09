package relation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : BLOCAGE D'UTILISATEURS
// ############################################################################

// ToggleBlock gère le blocage et le déblocage d'un utilisateur.
func ToggleBlock(ctx context.Context, callerID int64, input relation_models.RelationActionInput) error {

	// ── ÉTAPE 1 : RÈGLE MÉTIER (AUTO-BLOCAGE INTERDIT) ──────────────────────
	if callerID == input.TargetID {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Vous ne pouvez pas vous bloquer vous-même.", nil)
	}

	// ── ÉTAPE 2 : LECTURE DE L'ÉTAT ACTUEL (O(1) RAM L1) ────────────────────
	currentRelationState := cache_service.RelationValue(ctx, input.TargetID, callerID)

	var newRelationState int
	var redisActionType redis.ActionType

	// ── ÉTAPE 3 : LOGIQUE DE TRANSITION D'ÉTAT ──────────────────────────────
	if input.Action == variables.ActionBlockUser {
		if currentRelationState == variables.RelationStateBlocked {
			return nil // Idempotence : L'utilisateur est déjà bloqué.
		}

		newRelationState = variables.RelationStateBlocked
		if currentRelationState == variables.RelationStateNone {
			redisActionType = redis.ActionCreate // Création pure d'une relation de blocage
		} else {
			redisActionType = redis.ActionUpdate // Écrasement d'une amitié/abonnement par un blocage
		}
	} else if input.Action == variables.ActionUnblockUser {
		if currentRelationState != variables.RelationStateBlocked {
			return nil // Idempotence : L'utilisateur n'était pas bloqué.
		}

		newRelationState = variables.RelationStateNone
		redisActionType = redis.ActionDelete // Suppression physique de la relation
	} else {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Action de relation non reconnue.", nil)
	}

	// ── ÉTAPE 4 : MISE À JOUR IMMÉDIATE DU CACHE L1 ─────────────────────────
	// Retire automatiquement des Followers/Friends si le nouvel état est -1
	currentTime := domain.NowTime()
	errCache := cache_service.UpdateRelationState(ctx, input.TargetID, callerID, newRelationState, domain.TimeToMillis(currentTime))
	if errCache != nil {
		numan_log.Error(ctx).Err(errCache).Msg("Échec de la mise à jour du Cache L1 lors d'un ToggleBlock")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : PURGE DES RECOMMANDATIONS (CUCKOO & FEEDS) ────────────────
	// Si on bloque, on force l'algorithme à oublier ce qu'il a généré et
	// à exclure l'utilisateur bloqué au prochain Swipe.
	if input.Action == variables.ActionBlockUser {
		service.ResetCuckooFilter(ctx, callerID)
		service.ResetCuckooFilter(ctx, input.TargetID)

		_ = redis.FeedsPersonalized.DeleteObject(ctx, callerID)
		_ = redis.FeedsPersonalized.DeleteObject(ctx, input.TargetID)
	}

	// ── ÉTAPE 6 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	relationPayload := relation_models.RelationPayload{
		ID:          pkg.GenerateID(),
		PrimaryID:   callerID,
		SecondaryID: input.TargetID,
		State:       newRelationState,
		CreatedAt:   domain.TimeToMillis(currentTime),
		UpdatedAt:   domain.TimeToMillis(currentTime),
	}

	// PartitionKey = targetID pour assurer l'ordre chronologique des requêtes sur le profil cible
	errQueue := redis.EnqueueDB(ctx, relationPayload.ID, input.TargetID, redis.EntityRelation, redisActionType, relationPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("target_id", input.TargetID).Msg("Échec du Write-Behind pour ToggleBlock")
		return numan_error.NewInternal()
	}

	return nil
}
