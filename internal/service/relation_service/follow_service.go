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
// # SERVICE : ABONNEMENT (FOLLOW)
// ############################################################################

// ToggleFollow gère l'abonnement et le désabonnement avec idempotence en RAM.
func ToggleFollow(ctx context.Context, callerID int64, input relation_models.RelationActionInput) error {

	// ── ÉTAPE 1 : RÈGLE MÉTIER (AUTO-FOLLOW INTERDIT) ───────────────────────
	if callerID == input.TargetID {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Vous ne pouvez pas vous abonner à vous-même.", nil)
	}

	// ── ÉTAPE 2 : LECTURE DE L'ÉTAT ACTUEL (O(1) RAM L1) ────────────────────
	currentRelationState := cache_service.RelationValue(ctx, input.TargetID, callerID)

	// Sécurité absolue : Bloqué
	if currentRelationState == variables.RelationStateBlocked {
		return numan_error.NewForbidden(numan_error.CodeForbidden, "Action impossible : Utilisateur bloqué.", nil)
	}

	newRelationState := currentRelationState
	redisActionType := redis.ActionCreate

	// ── ÉTAPE 3 : LOGIQUE DE TRANSITION ET IDEMPOTENCE (SPAM CLIC) ──────────
	if input.Action == variables.ActionSubcribeUser {
		if currentRelationState == variables.RelationStateFollow || currentRelationState == variables.RelationStateFriend {
			return nil // Déjà suivi ou ami, coupe-circuit instantané (Zéro I/O BDD).
		}
		newRelationState = variables.RelationStateFollow
		redisActionType = redis.ActionCreate

	} else if input.Action == variables.ActionUnSubcribeUser {
		if currentRelationState == variables.RelationStateNone {
			return nil // Déjà aucun lien, coupe-circuit.
		}
		newRelationState = variables.RelationStateNone
		redisActionType = redis.ActionDelete

	} else {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Action de relation non reconnue.", nil)
	}

	// ── ÉTAPE 4 : MISE À JOUR IMMÉDIATE DU CACHE L1 ─────────────────────────
	// Impacte instantanément le rendu UI et les futurs Fan-Outs de posts.
	currentTime := domain.NowTime()
	errCache := cache_service.UpdateRelationState(ctx, input.TargetID, callerID, newRelationState, domain.TimeToMillis(currentTime))
	if errCache != nil {
		numan_log.Error(ctx).Err(errCache).Msg("Échec de la mise à jour du Cache L1 lors d'un ToggleFollow")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	relationPayload := relation_models.RelationPayload{
		ID:          pkg.GenerateID(),
		PrimaryID:   callerID,
		SecondaryID: input.TargetID,
		State:       newRelationState,
		CreatedAt:   domain.TimeToMillis(currentTime),
		UpdatedAt:   domain.TimeToMillis(currentTime),
	}

	// PartitionKey = targetID pour centraliser les requêtes sur le shard de la cible.
	errQueue := redis.EnqueueDB(ctx, relationPayload.ID, input.TargetID, redis.EntityRelation, redisActionType, relationPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("target_id", input.TargetID).Msg("Échec du Write-Behind pour ToggleFollow")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : DISTRIBUTION DES NOTIFICATIONS ────────────────────────────
	if newRelationState == variables.RelationStateFollow && currentRelationState == variables.RelationStateNone {
		go func() {
			backgroundCtx := context.Background()
			errNotif := notification_service.DispatchNotification(backgroundCtx, input.TargetID, callerID, variables.EventRelationFollowed, callerID)
			if errNotif != nil {
				numan_log.Error(ctx).Err(errNotif).Msg("Échec de l'expédition de la notification pour Follow")
			}
		}()
	}

	return nil
}
