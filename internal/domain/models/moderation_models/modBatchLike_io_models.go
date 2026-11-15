package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ModBatchLikeInput struct {
	AuthorUserIDs []int64 `json:"author_user_ids" binding:"required,min=1,max=500,dive,gt=0"`
	Reason        string  `json:"reason,omitempty" binding:"omitempty,max=500"`
}

// Validate vérifie les identifiants auteurs et la raison optionnelle du batch-like modérateur.
func (p *ModBatchLikeInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de validation groupée est vide.", nil)
	}
	if len(p.AuthorUserIDs) == 0 || len(p.AuthorUserIDs) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste d'auteurs doit contenir entre 1 et 500 identifiants.", nil)
	}
	for _, id := range p.AuthorUserIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque identifiant d'auteur doit être strictement positif.", nil)
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Reason)) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif ne peut pas dépasser 500 caractères.", nil)
	}
	return nil
}

type ModBatchLikeOutput struct {
	EventID    int64 `json:"event_id"`
	UsersCount int   `json:"users_count"`
	QueuedAt   int64 `json:"queued_at"`
}
