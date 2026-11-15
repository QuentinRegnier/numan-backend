package lite_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// TagPostItem représente la structure ultra-légère stockée et compressée
// dans le speed cache Redis (ZSETs ou listes) pour lier un post à un tag
// tout en conservant son contexte de création (Direct ou Indirect).
type TagPostItem struct {
	PostID     int64 `bson:"post_id" json:"post_id" msgpack:"post_id"`
	IsIndirect bool  `bson:"is_indirect" json:"is_indirect" msgpack:"is_indirect"`
}

// Validate vérifie que l'entrée de tag pointe vers un post valide.
func (p *TagPostItem) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'élément tag-post est nul.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de publication de l'élément tag-post doit être strictement positif.", nil)
	}
	return nil
}
