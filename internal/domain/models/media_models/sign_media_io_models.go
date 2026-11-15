package media_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// SignMediaInput valide la demande de signature d'un média.
// L'application doit fournir l'ID du média ET le contexte (Post ou Conversation)
// dans lequel elle l'a vu, afin que le serveur puisse vérifier ses droits d'accès.
type SignMediaInput struct {
	MediaID        int64 `json:"media_id" binding:"required,gt=0"`
	PostID         int64 `json:"post_id" binding:"omitempty,gte=0"`
	ConversationID int64 `json:"conversation_id" binding:"omitempty,gte=0"`
}

// Validate vérifie la validité des identifiants et l'absence d'ambiguïté de contexte.
func (p *SignMediaInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La demande de signature média est nulle.", nil)
	}
	if p.MediaID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média doit être strictement positif.", nil)
	}
	if p.PostID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication ne peut pas être négatif.", nil)
	}
	if p.ConversationID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la conversation ne peut pas être négatif.", nil)
	}
	if p.PostID > 0 && p.ConversationID > 0 {
		return numan_error.NewBadRequest(
			"AMBIGUOUS_CONTEXT",
			"Impossible de spécifier simultanément un post_id et un conversation_id.",
			nil,
		)
	}
	return nil
}

// SignMediaOutput renvoie l'URL finale signée cryptographiquement avec HMAC.
type SignMediaOutput struct {
	MediaID int64  `json:"media_id"`
	URL     string `json:"url"`
}
