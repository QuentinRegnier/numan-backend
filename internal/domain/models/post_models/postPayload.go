package post_models

import (
	"fmt"
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// Structure interne correspondant exactement au schéma Postgres content.posts
type PostPayload struct {
	ID                int64     `bson:"id" json:"id" msgpack:"id"`
	UserID            int64     `bson:"user_id" json:"user_id" msgpack:"user_id"`
	Content           string    `bson:"content" json:"content" msgpack:"content"`
	Hashtags          []string  `bson:"hashtags" json:"hashtags" msgpack:"hashtags"`
	IndirectHashtags  []string  `bson:"indirect_hashtags" json:"indirect_hashtags" msgpack:"indirect_hashtags"` // ✅ NOUVEAU
	Identifiers       []int64   `bson:"identifiers" json:"identifiers" msgpack:"identifiers"`
	MediaIDs          []int64   `bson:"media_ids" json:"media_ids" msgpack:"media_ids"`
	Visibility        int       `bson:"visibility" json:"visibility" msgpack:"visibility"`
	PriorityLevel     int       `bson:"priority_level" json:"priority_level" msgpack:"priority_level"`
	Location          string    `bson:"location" json:"location" msgpack:"location"`
	LikeCount         int       `bson:"like_count" json:"like_count" msgpack:"like_count"`
	CommentCount      int       `bson:"comment_count" json:"comment_count" msgpack:"comment_count"`
	ViewCount         int       `bson:"view_count" json:"view_count" msgpack:"view_count"`
	ReportCount       int       `bson:"report_count" json:"-" msgpack:"report_count"`
	HasMedia          bool      `bson:"has_media" json:"has_media" msgpack:"has_media"`
	IAScoreMax        float64   `bson:"ia_score_max" json:"ia_score_max" msgpack:"ia_score_max"` // MAX(P_ia) des 1 à 4 images
	Vector            []float32 `bson:"vector" json:"vector" msgpack:"vector"`
	VectorVersion     int       `bson:"vector_version" json:"vector_version" msgpack:"vector_version"`
	TelemetryDwellSum float64   `bson:"telemetry_dwell_sum" json:"telemetry_dwell_sum" msgpack:"telemetry_dwell_sum"`
	TelemetryDwellSq  float64   `bson:"telemetry_dwell_sq" json:"telemetry_dwell_sq" msgpack:"telemetry_dwell_sq"`
	TelemetryClicks   int       `bson:"telemetry_clicks" json:"telemetry_clicks" msgpack:"telemetry_clicks"`
	CreatedAt         int64     `bson:"created_at" json:"created_at" msgpack:"created_at"`
	UpdatedAt         int64     `bson:"updated_at" json:"updated_at" msgpack:"updated_at"`
}

// Validate vérifie les invariants métier d'un PostPayload avant écriture en cache L1 ou persistance.
func (p *PostPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de publication est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'auteur de la publication doit être strictement positif.", nil)
	}
	switch p.Visibility {
	case variables.PostVisibilityModerated,
		variables.PostVisibilityDeleted,
		variables.PostVisibilityAll,
		variables.PostVisibilitySubcriber,
		variables.PostVisibilityFriend,
		variables.PostVisibilityQuarantine:
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut de visibilité de la publication est invalide.", nil)
	}
	if p.LikeCount < 0 || p.CommentCount < 0 || p.ViewCount < 0 || p.ReportCount < 0 || p.TelemetryClicks < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les compteurs d'une publication ne peuvent pas être négatifs.", nil)
	}
	if math.IsNaN(p.IAScoreMax) || math.IsInf(p.IAScoreMax, 0) || p.IAScoreMax < 0.0 || p.IAScoreMax > 1.0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score IA maximal doit être compris entre 0.0 et 1.0.", nil)
	}
	if len(p.Hashtags) > variables.MaxHashtagsPerPost+1 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le nombre de hashtags dépasse la limite autorisée (%d).", variables.MaxHashtagsPerPost),
			nil,
		)
	}
	if len(p.Identifiers) > variables.MaxIdentifiersPerPost {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le nombre d'identifiants dépasse la limite autorisée (%d).", variables.MaxIdentifiersPerPost),
			nil,
		)
	}
	for _, id := range p.Identifiers {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants taggés doivent être strictement positifs.", nil)
		}
	}
	if len(p.MediaIDs) > variables.MaxMediaPerPost {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le nombre de médias dépasse la limite autorisée (%d).", variables.MaxMediaPerPost),
			nil,
		)
	}
	for _, mid := range p.MediaIDs {
		if mid <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants de médias doivent être strictement positifs.", nil)
		}
	}
	return nil
}
