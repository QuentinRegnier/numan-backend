package telemetry_models

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// SyncTelemetryPayload correspond à la fusion TDD § 2.5 (Vecteur) + Télémétrie d'engagement.
type SyncTelemetryPayload struct {
	SchemaVersion int `json:"schema_version"`

	// Bloc 1 : Edge Vector (TDD § 2.5)
	Vector struct {
		Dims             int       `json:"dims"`
		EmbeddingVersion int       `json:"embedding_version"`
		Values           []float32 `json:"values"`
		NormPreUnit      float32   `json:"norm_pre_unit"`
	} `json:"vector"`

	// Bloc 2 : Méta-données et Confiance
	Meta struct {
		TotalInteractions int     `json:"total_interactions"`
		ConfidenceScore   float64 `json:"confidence_score"`
		ClientPlatform    string  `json:"client_platform"`
		ClientUpdatedAt   int64   `json:"client_updated_at"`
	} `json:"meta"`

	// Bloc 3 : Top Tags pour le Magasinier
	TopTags []string `json:"top_tags"`

	// Bloc 4 : Matrice d'Engagement (Batch)
	Telemetry []TelemetryEvent `json:"telemetry"`
}

// Validate vérifie l'intégrité complète du payload de synchronisation télémétrique.
func (p *SyncTelemetryPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de télémétrie est nul.", nil)
	}
	if p.SchemaVersion < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La version du schéma télémétrique ne peut pas être négative.", nil)
	}
	if p.Vector.Dims < 0 || p.Vector.EmbeddingVersion < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les métadonnées vectorielles ne peuvent pas être négatives.", nil)
	}
	if math.IsNaN(float64(p.Vector.NormPreUnit)) || math.IsInf(float64(p.Vector.NormPreUnit), 0) || p.Vector.NormPreUnit < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La norme pré-unitaire du vecteur est invalide.", nil)
	}
	if len(p.Vector.Values) > 0 {
		if len(p.Vector.Values) != variables.VectorDimTotal {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La dimension du vecteur télémétrique est invalide.", nil)
		}
		if p.Vector.Dims != 0 && p.Vector.Dims != len(p.Vector.Values) {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La dimension déclarée du vecteur ne correspond pas au nombre de composantes.", nil)
		}
		for _, v := range p.Vector.Values {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le vecteur télémétrique contient une valeur non finie.", nil)
			}
		}
	}
	if p.Meta.TotalInteractions < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre total d'interactions ne peut pas être négatif.", nil)
	}
	if math.IsNaN(p.Meta.ConfidenceScore) || math.IsInf(p.Meta.ConfidenceScore, 0) || p.Meta.ConfidenceScore < variables.TrustScoreMin || p.Meta.ConfidenceScore > variables.TrustScoreMax {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score de confiance client doit être compris entre 0.0 et 1.0.", nil)
	}
	if p.Meta.ClientUpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage client ne peut pas être négatif.", nil)
	}
	if utf8.RuneCountInString(p.Meta.ClientPlatform) > 64 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La plateforme client ne peut pas dépasser 64 caractères.", nil)
	}
	if len(p.TopTags) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste top_tags ne peut pas dépasser 100 éléments.", nil)
	}
	for _, tag := range p.TopTags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" || utf8.RuneCountInString(trimmed) > 64 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque tag de top_tags doit être non vide et ne pas dépasser 64 caractères.", nil)
		}
	}
	if len(p.Telemetry) > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le lot d'événements de télémétrie ne peut pas dépasser 1000 entrées.", nil)
	}
	for i := range p.Telemetry {
		if err := p.Telemetry[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}
