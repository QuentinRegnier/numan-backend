package variables_models

import (
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// UpdateVariablesInput représente le payload JSON partiel envoyé à PATCH /admin/variables/update.
// Chaque clé correspond au nom d'une variable autorisée et chaque valeur à sa nouvelle valeur JSON brute.
type UpdateVariablesInput map[string]json.RawMessage

// Validate vérifie que le patch de variables est non vide et conforme au registre dynamique.
func (p *UpdateVariablesInput) Validate() error {
	if p == nil || len(*p) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Aucune variable fournie dans le corps de la requête.", nil)
	}
	_, err := variables.ValidateAndPreparePatch(*p)
	return err
}

// UpdateVariablesOutput structure la réponse de la route PATCH /admin/variables/update.
type UpdateVariablesOutput struct {
	Message          string         `json:"message"`
	Version          int64          `json:"version"`
	UpdatedVariables map[string]any `json:"updated_variables"`
}
