package variables_models

// VariableDetail expose les métadonnées complètes d'une variable dynamique pour le panel d'administration.
type VariableDetail struct {
	Value        any    `json:"value"`
	DefaultValue any    `json:"default_value"`
	Type         string `json:"type"`
	Category     string `json:"category"`
	IsCustom     bool   `json:"is_custom"`
}

// GetVariablesOutput structure la réponse de la route GET /admin/variables/get.
type GetVariablesOutput struct {
	Version   int64                     `json:"version"`
	Variables map[string]any            `json:"variables"`
	Details   map[string]VariableDetail `json:"details"`
}
