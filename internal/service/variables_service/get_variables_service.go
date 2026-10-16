package variables_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/variables_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetVariables vérifie les droits administrateur, s'assure de la fraîcheur du cache par rapport à Redis,
// et retourne l'intégralité des variables dynamiques avec leurs valeurs actuelles et métadonnées.
func GetVariables(ctx context.Context, callerID int64) (variables_models.GetVariablesOutput, error) {
	// 1. CONTRÔLE D'ACCÈS ZERO-TRUST (Grade Administrateur requis)
	if err := security_service.LeftAdmin(ctx, callerID); err != nil {
		return variables_models.GetVariablesOutput{}, err
	}

	// 2. VÉRIFICATION DE FRAÎCHEUR AVEC REDIS (Convergence Read-Your-Writes)
	EnsureVariablesFreshness(ctx)

	// 3. LECTURE ATOMIQUE DU SNAPSHOT EN MÉMOIRE
	rawValues, rawDetails, version := variables.GetSnapshot()

	detailsMap := make(map[string]variables_models.VariableDetail, len(rawDetails))
	for k, d := range rawDetails {
		detailsMap[k] = variables_models.VariableDetail{
			Value:        d.Value,
			DefaultValue: d.DefaultValue,
			Type:         d.Type,
			Category:     d.Category,
			IsCustom:     d.IsCustom,
		}
	}

	return variables_models.GetVariablesOutput{
		Version:   version,
		Variables: rawValues,
		Details:   detailsMap,
	}, nil
}
