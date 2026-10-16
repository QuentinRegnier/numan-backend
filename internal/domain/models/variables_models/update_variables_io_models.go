package variables_models

import "encoding/json"

// UpdateVariablesInput représente le payload JSON partiel envoyé à PATCH /admin/variables/update.
// Chaque clé correspond au nom d'une variable autorisée et chaque valeur à sa nouvelle valeur JSON brute.
type UpdateVariablesInput map[string]json.RawMessage

// UpdateVariablesOutput structure la réponse de la route PATCH /admin/variables/update.
type UpdateVariablesOutput struct {
	Message          string         `json:"message"`
	Version          int64          `json:"version"`
	UpdatedVariables map[string]any `json:"updated_variables"`
}
