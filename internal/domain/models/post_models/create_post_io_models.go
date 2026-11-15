package post_models

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type CreatePostInput struct {
	Content     string    `json:"content" binding:"max=2200"`
	Hashtags    []string  `json:"hashtags" binding:"max=10,dive,alphanum,max=20"`
	Identifiers []int64   `json:"identifiers" binding:"max=10"`
	MediaIDs    []int64   `json:"media_ids" binding:"max=4"`                                      // Maximum 4 médias par post
	IAScores    []float64 `json:"ia_scores,omitempty" binding:"omitempty,max=4,dive,min=0,max=1"` // Scores NSFWJS Porn [0.0, 1.0] des images
	Location    string    `json:"location" binding:"max=100"`
	Visibility  int       `json:"visibility" binding:"oneof=0 1 2"` // 0: Public, 1: Abonnés, 2: Amis (Mode 3 Privé supprimé)
}

// Validate vérifie les règles métier de création d'une publication.
func (p *CreatePostInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de création de publication est nul.", nil)
	}
	if utf8.RuneCountInString(p.Content) > 2200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le contenu de la publication ne peut pas dépasser 2200 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Location) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La localisation ne peut pas dépasser 100 caractères.", nil)
	}
	if len(p.MediaIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un média est requis.", nil)
	}
	uniqueMedia := make(map[int64]struct{}, len(p.MediaIDs))
	for _, mid := range p.MediaIDs {
		if mid <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants de médias doivent être strictement positifs.", nil)
		}
		uniqueMedia[mid] = struct{}{}
	}
	if len(uniqueMedia) > variables.MaxMediaPerPost {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de médias dépasse la limite autorisée.", nil)
	}

	uniqueHashtags := make(map[string]struct{}, len(p.Hashtags))
	for _, h := range p.Hashtags {
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un hashtag ne peut pas être vide.", nil)
		}
		if utf8.RuneCountInString(trimmed) > 20 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un hashtag ne peut pas dépasser 20 caractères.", nil)
		}
		uniqueHashtags[trimmed] = struct{}{}
	}
	if len(uniqueHashtags) > variables.MaxHashtagsPerPost {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de hashtags dépasse la limite autorisée.", nil)
	}

	uniqueIdentifiers := make(map[int64]struct{}, len(p.Identifiers))
	for _, id := range p.Identifiers {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants taggés doivent être strictement positifs.", nil)
		}
		uniqueIdentifiers[id] = struct{}{}
	}
	if len(uniqueIdentifiers) > variables.MaxIdentifiersPerPost {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre d'identifiants dépasse la limite autorisée.", nil)
	}

	if len(p.IAScores) > variables.MaxMediaPerPost {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			fmt.Sprintf("Le nombre de scores IA ne peut pas dépasser %d.", variables.MaxMediaPerPost),
			nil,
		)
	}
	for _, score := range p.IAScores {
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0.0 || score > 1.0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque score IA doit être compris entre 0.0 et 1.0.", nil)
		}
	}

	if p.Visibility < variables.PostVisibilityAll || p.Visibility > variables.PostVisibilityFriend {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Niveau de visibilité non supporté.", nil)
	}
	return nil
}

type CreatePostResponse struct {
	PostID             int64  `json:"post_id"`
	Visibility         int    `json:"visibility"`                    // 3 si placé en quarantaine (Shadowban), sinon 0/1/2
	RequiresModeration bool   `json:"requires_moderation,omitempty"` // true si TrustScore < 0.40
	Message            string `json:"message,omitempty"`
}
