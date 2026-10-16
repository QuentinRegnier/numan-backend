package variables_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/variables_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// UpdateVariables valide un lot partiel de variables, délègue au Cache Service la persistance atomique Redis
// et la diffusion Pub/Sub, puis actualise la mémoire locale.
func UpdateVariables(
	ctx context.Context,
	callerID int64,
	input variables_models.UpdateVariablesInput,
) (variables_models.UpdateVariablesOutput, error) {
	// 1. CONTRÔLE D'ACCÈS ZERO-TRUST (Grade Administrateur requis)
	if err := security_service.LeftAdmin(ctx, callerID); err != nil {
		return variables_models.UpdateVariablesOutput{}, err
	}

	// 2. PHASE 1 : VALIDATION STRICTE EN MÉMOIRE (Zéro effet de bord si une clé ou un type est invalide)
	preparedPatch, errValidation := variables.ValidateAndPreparePatch(input)
	if errValidation != nil {
		return variables_models.UpdateVariablesOutput{}, errValidation
	}

	// 3. PHASE 2 : PERSISTANCE ATOMIQUE REDIS + DIFFUSION PUB/SUB VIA LE CACHE SERVICE
	newVersion, errCache := cache_service.ApplyAndBroadcastDynamicVariablesPatch(ctx, preparedPatch)
	if errCache != nil {
		numan_log.Error(ctx).Err(errCache).Int64("admin_id", callerID).Msg("Variables dynamiques : échec de la persistance Redis")
		return variables_models.UpdateVariablesOutput{}, numan_error.NewInternal(errCache)
	}

	// 4. MISE À JOUR LOCALE COHÉRENTE
	if errSync := SyncVariablesFromRedis(ctx); errSync != nil {
		variables.ApplyPreparedPatchInMemory(preparedPatch, newVersion)
	}

	// 5. JOURNALISATION D'AUDIT SÉCURISÉE
	modifiedKeys := make([]string, 0, len(preparedPatch.Changes))
	for _, ch := range preparedPatch.Changes {
		modifiedKeys = append(modifiedKeys, ch.CanonicalName)
	}
	numan_log.Info(ctx).
		Int64("admin_id", callerID).
		Int64("version", newVersion).
		Interface("modified_keys", modifiedKeys).
		Action(numan_log.ActionUpdate).
		Msg("Variables dynamiques mises à jour avec succès")

	return variables_models.UpdateVariablesOutput{
		Message:          "Variables mises à jour et synchronisées avec succès.",
		Version:          newVersion,
		UpdatedVariables: preparedPatch.Updated,
	}, nil
}
