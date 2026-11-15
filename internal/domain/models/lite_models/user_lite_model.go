package lite_models

import (
	"math"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type UserLiteRequest struct {
	ID                      int64      `bson:"id" json:"id"`
	Username                string     `bson:"username" json:"username"`
	FirstName               string     `bson:"first_name" json:"first_name"`
	LastName                string     `bson:"last_name" json:"last_name"`
	ProfilePictureID        int64      `bson:"profile_picture_id" json:"profile_picture_id"`
	Bio                     string     `bson:"bio" json:"bio"`
	Grade                   int        `bson:"grade" json:"grade"`
	TrustScore              float64    `bson:"trust_score" json:"trust_score" msgpack:"trust_score"`
	ModeratorTrustScore     float64    `bson:"moderator_trust_score" json:"moderator_trust_score" msgpack:"moderator_trust_score"`
	BlameCount              int        `bson:"blame_count" json:"blame_count" msgpack:"blame_count"`
	QuarantinePostsApproved int        `bson:"quarantine_posts_approved" json:"quarantine_posts_approved" msgpack:"quarantine_posts_approved"`
	Badges                  UserBadges `bson:"badges" json:"badges" msgpack:"badges"`
	ConversationPermission  int        `bson:"conversation_permission" json:"conversation_permission"`
	AddGroupPermission      int        `bson:"add_group_permission" json:"add_group_permission"`
	HideConnections         int        `bson:"hide_connections" json:"hide_connections" msgpack:"hide_connections"`
	ShowOnlineStatus        bool       `bson:"show_online_status" json:"show_online_status" msgpack:"show_online_status"`
	AllowTagging            int        `bson:"allow_tagging" json:"allow_tagging" msgpack:"allow_tagging"`
	AllowMentions           int        `bson:"allow_mentions" json:"allow_mentions" msgpack:"allow_mentions"`
}

// Validate vérifie les invariants métier d'un utilisateur allégé en cache.
func (p *UserLiteRequest) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload allégé utilisateur est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.Username) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'utilisateur est obligatoire.", nil)
	}
	if p.ProfilePictureID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la photo de profil ne peut pas être négatif.", nil)
	}
	if p.Grade < variables.UserGradeUser || p.Grade > variables.UserGradeAdmin {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le grade utilisateur est invalide.", nil)
	}
	if math.IsNaN(p.TrustScore) || math.IsInf(p.TrustScore, 0) || p.TrustScore < variables.TrustScoreMin || p.TrustScore > variables.TrustScoreMax {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score de confiance doit être compris entre 0.0 et 1.0.", nil)
	}
	if math.IsNaN(p.ModeratorTrustScore) || math.IsInf(p.ModeratorTrustScore, 0) || p.ModeratorTrustScore < variables.TrustScoreMin || p.ModeratorTrustScore > variables.TrustScoreMax {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score de confiance modérateur doit être compris entre 0.0 et 1.0.", nil)
	}
	if p.BlameCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de blâmes ne peut pas être négatif.", nil)
	}
	if p.QuarantinePostsApproved < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le compteur de publications approuvées ne peut pas être négatif.", nil)
	}
	if p.Badges != nil {
		if err := p.Badges.Validate(); err != nil {
			return err
		}
	}
	if p.ConversationPermission < 0 || p.ConversationPermission > 3 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission de conversation est invalide.", nil)
	}
	if p.AddGroupPermission < 0 || p.AddGroupPermission > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission d'ajout aux groupes est invalide.", nil)
	}
	if p.HideConnections < variables.ConnectionsVisibilityPublic || p.HideConnections > variables.ConnectionsVisibilityPrivate {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La visibilité des relations est invalide.", nil)
	}
	if p.AllowTagging < variables.TagPermissionEveryone || p.AllowTagging > variables.TagPermissionNobody {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission de marquage (tagging) est invalide.", nil)
	}
	if p.AllowMentions < variables.TagPermissionEveryone || p.AllowMentions > variables.TagPermissionNobody {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission de mention est invalide.", nil)
	}
	return nil
}
