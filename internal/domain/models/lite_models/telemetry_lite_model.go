package lite_models

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type TelemetryProfileLite struct {
	Vector          []float32 `msgpack:"vector" json:"vector" bson:"vector"`
	TopTags         []string  `msgpack:"top_tags" json:"top_tags" bson:"top_tags"`
	ConfidenceScore float64   `msgpack:"confidence_score" json:"confidence_score" bson:"confidence_score"`
	TimestampMs     int64     `msgpack:"timestamp_ms" json:"timestamp_ms" bson:"timestamp_ms"`
}

// Validate vérifie les invariants métier d'un profil télémétrique allégé.
func (p *TelemetryProfileLite) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le profil télémétrique allégé est nul.", nil)
	}
	if len(p.Vector) > 0 {
		if len(p.Vector) != variables.VectorDimTotal {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La dimension du vecteur télémétrique est invalide.", nil)
		}
		for _, v := range p.Vector {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le vecteur télémétrique contient une valeur non finie.", nil)
			}
		}
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
	if math.IsNaN(p.ConfidenceScore) || math.IsInf(p.ConfidenceScore, 0) || p.ConfidenceScore < variables.TrustScoreMin || p.ConfidenceScore > variables.TrustScoreMax {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score de confiance doit être compris entre 0.0 et 1.0.", nil)
	}
	if p.TimestampMs < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage télémétrique ne peut pas être négatif.", nil)
	}
	return nil
}
