package relation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : AMITIÉ (FRIENDS)
// ############################################################################

// ToggleFriend gère les ajouts et retraits d'amis entre utilisateurs.
func ToggleFriend(ctx context.Context, callerID int64, input relation_models.RelationActionInput) error {

	// ── ÉTAPE 1 : RÈGLE MÉTIER (AUTO-AMITIÉ INTERDITE) ──────────────────────
	if callerID == input.TargetID {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Vous ne pouvez pas être ami avec vous-même.", nil)
	}

	// ── ÉTAPE 2 : LECTURE DE L'ÉTAT ACTUEL (O(1) RAM L1) ────────────────────
	currentRelationState := cache_service.RelationValue(ctx, input.TargetID, callerID)

	if currentRelationState == variables.RelationStateBlocked {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Action impossible : Utilisateur bloqué.", nil)
	}

	var newRelationState int
	var redisActionType redis.ActionType

	// ── ÉTAPE 3 : LOGIQUE DE TRANSITION ET IDEMPOTENCE ──────────────────────
	if input.Action == variables.ActionPromoteToFriend {
		if currentRelationState == variables.RelationStateFriend {
			return nil // Déjà ami, coupe-circuit.
		}

		newRelationState = variables.RelationStateFriend

		if currentRelationState == variables.RelationStateNone {
			redisActionType = redis.ActionCreate // Pas encore abonné, on crée la relation d'amitié directement.
		} else {
			redisActionType = redis.ActionUpdate // Déjà abonné, on promeut la relation.
		}

	} else if input.Action == variables.ActionDemoteToFollower {
		if currentRelationState == variables.RelationStateNone || currentRelationState == variables.RelationStateFollow {
			return nil // N'est déjà pas/plus ami, coupe-circuit.
		}

		// S'il était ami (2), le retrait le déclasse en simple abonné (1).
		newRelationState = variables.RelationStateFollow
		redisActionType = redis.ActionUpdate

	} else {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Action de relation non reconnue.", nil)
	}

	// ── ÉTAPE 4 : MISE À JOUR IMMÉDIATE DU CACHE L1 ─────────────────────────
	currentTime := domain.NowTime()
	errCache := cache_service.UpdateRelationState(ctx, callerID, input.TargetID, newRelationState, domain.TimeToMillis(currentTime))
	if errCache != nil {
		numan_log.Error(ctx).Err(errCache).Msg("Échec de la mise à jour du Cache L1 lors d'un ToggleFriend")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	relationPayload := relation_models.RelationPayload{
		ID:          pkg.GenerateID(), // ID virtuel, l'update BDD se fera sur PrimaryID / SecondaryID.
		PrimaryID:   input.TargetID,
		SecondaryID: callerID,
		State:       newRelationState,
		CreatedAt:   domain.TimeToMillis(currentTime),
		UpdatedAt:   domain.TimeToMillis(currentTime),
	}

	// PartitionKey = targetID pour assurer l'ordre chronologique des requêtes.
	errQueue := redis.EnqueueDB(ctx, relationPayload.ID, input.TargetID, redis.EntityRelation, redisActionType, relationPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("target_id", input.TargetID).Msg("Échec du Write-Behind pour ToggleFriend")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : DISTRIBUTION DES NOTIFICATIONS ────────────────────────────
	if input.Action == variables.ActionPromoteToFriend {
		go func() {
			backgroundCtx := context.Background()
			errNotif := notification_service.DispatchNotification(backgroundCtx, input.TargetID, callerID, variables.EventFriendshipEst, callerID)
			if errNotif != nil {
				numan_log.Error(ctx).Err(errNotif).Msg("Échec de l'expédition de la notification d'amitié")
			}
		}()
	}

	return nil
}
