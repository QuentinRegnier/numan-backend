package auth_models

import (
	"math"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type UserPayload struct {
	Badges                  UserBadges `bson:"badges" json:"badges"`
	Username                string     `bson:"username" json:"username"`
	Email                   string     `bson:"email" json:"email"`
	Phone                   string     `bson:"phone" json:"phone"`
	PasswordHash            string     `bson:"password_hash" json:"password_hash"`
	TOTPSecret              string     `bson:"totp_secret" json:"totp_secret" msgpack:"totp_secret"`
	FirstName               string     `bson:"first_name" json:"first_name"`
	LastName                string     `bson:"last_name" json:"last_name"`
	Bio                     string     `bson:"bio" json:"bio"`
	Location                string     `bson:"location" json:"location"`
	School                  string     `bson:"school" json:"school"`
	Work                    string     `bson:"work" json:"work"`
	BanReason               string     `bson:"ban_reason" json:"ban_reason"`
	ID                      int64      `bson:"id" json:"id"`
	Birthdate               int64      `bson:"birthdate" json:"birthdate"`
	ProfilePictureID        int64      `bson:"profile_picture_id" json:"profile_picture_id"`
	BanExpiresAt            int64      `bson:"ban_expires_at" json:"ban_expires_at"`
	LastInfractionAt        int64      `bson:"last_infraction_at" json:"last_infraction_at" msgpack:"last_infraction_at"`
	CreatedAt               int64      `bson:"created_at" json:"created_at"`
	UpdatedAt               int64      `bson:"updated_at" json:"updated_at"`
	TrustScore              float64    `bson:"trust_score" json:"trust_score" msgpack:"trust_score"`
	ModeratorTrustScore     float64    `bson:"moderator_trust_score" json:"moderator_trust_score" msgpack:"moderator_trust_score"`
	BlameCount              int        `bson:"blame_count" json:"blame_count" msgpack:"blame_count"`
	QuarantinePostsApproved int        `bson:"quarantine_posts_approved" json:"quarantine_posts_approved" msgpack:"quarantine_posts_approved"`
	TwoFactorMethod         int        `bson:"two_factor_method" json:"two_factor_method" msgpack:"two_factor_method"`
	Sex                     int        `bson:"sex" json:"sex"`
	Grade                   int        `bson:"grade" json:"grade"`
	TwoFactorEnabled        bool       `bson:"two_factor_enabled" json:"two_factor_enabled" msgpack:"two_factor_enabled"`
	EmailVerified           bool       `bson:"email_verified" json:"email_verified"`
	PhoneVerified           bool       `bson:"phone_verified" json:"phone_verified"`
	Desactivated            bool       `bson:"desactivated" json:"desactivated"`
	Banned                  bool       `bson:"banned" json:"banned"`
}

// Validate vérifie les invariants métier fondamentaux d'un UserPayload avant mise en cache ou persistance.
func (p *UserPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload utilisateur est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.Username) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'utilisateur est obligatoire.", nil)
	}
	if strings.TrimSpace(p.Email) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'adresse email est obligatoire.", nil)
	}
	if strings.TrimSpace(p.PasswordHash) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le hash du mot de passe est obligatoire.", nil)
	}
	if p.ProfilePictureID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la photo de profil ne peut pas être négatif.", nil)
	}
	if p.Sex < 0 || p.Sex > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le genre utilisateur est invalide.", nil)
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
	if p.TwoFactorMethod < 0 || p.TwoFactorMethod > (variables.TwoFactorBitEmail|variables.TwoFactorBitTOTP|variables.TwoFactorBitPasskey) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le masque 2FA utilisateur est invalide.", nil)
	}
	if p.Badges != nil {
		if err := p.Badges.Validate(); err != nil {
			return err
		}
	}
	return nil
}
