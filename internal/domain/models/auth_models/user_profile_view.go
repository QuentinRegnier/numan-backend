package auth_models

// UserProfileView est le DTO sécurisé envoyé au client.
// Il exclut physiquement toutes les données de modération et les secrets.
type UserProfileView struct {
	Badges           UserBadges `json:"badges"`
	Username         string     `json:"username"`
	Email            string     `json:"email"`
	Phone            string     `json:"phone"`
	FirstName        string     `json:"first_name"`
	LastName         string     `json:"last_name"`
	Bio              string     `json:"bio"`
	Location         string     `json:"location"`
	School           string     `json:"school"`
	Work             string     `json:"work"`
	ID               int64      `json:"id"`
	TwoFactorMethod  int        `json:"two_factor_method"`
	Birthdate        int64      `json:"birthdate"`
	Sex              int        `json:"sex"`
	Grade            int        `json:"grade"`
	CreatedAt        int64      `json:"created_at"`
	UpdatedAt        int64      `json:"updated_at"`
	TwoFactorEnabled bool       `json:"two_factor_enabled"`
	IsOnline         bool       `json:"is_online"` // NOUVEAU : Statut de présence
}
