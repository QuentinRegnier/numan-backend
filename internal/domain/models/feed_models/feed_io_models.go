package feed_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// GetFeedInput valide la requête de scroll ou de refresh
type GetFeedInput struct {
	Force         bool `json:"force" form:"force"` // Détecté via le suffixe /force, query param ?force=true, ou champ JSON
	LastSeenIndex int  `json:"last_seen_index" form:"last_seen_index,default=0"`
}

// Validate vérifie la validité de l'index de pagination du fil d'actualité.
func (p *GetFeedInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de récupération du fil d'actualité sont nuls.", nil)
	}
	if p.LastSeenIndex < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'index de pagination (last_seen_index) doit être positif ou nul.", nil)
	}
	return nil
}

// GetFeedOutput est la réponse riche renvoyée au client mobile
type GetFeedOutput struct {
	Status        string                      `json:"status"`          // Message de confirmation
	ActiveFeed    string                      `json:"active_feed"`     // Indique "A", "B", ou "C"
	LastSeenIndex int                         `json:"last_seen_index"` // Le nouveau curseur du client
	Posts         []post_models.GetPostOutput `json:"posts"`           // La donnée riche (Hydratée, signée, filtrée)
}
