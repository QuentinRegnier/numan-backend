package auth_models

// UserProfileView est le DTO sécurisé envoyé au client.
// Il exclut physiquement toutes les données de modération et les secrets.
type UserProfileView struct {
	ID        int64      `json:"id"`
	Username  string     `json:"username"`
	Email     string     `json:"email"`
	Phone     string     `json:"phone"`
	FirstName string     `json:"first_name"`
	LastName  string     `json:"last_name"`
	Birthdate int64      `json:"birthdate"`
	Sex       int        `json:"sex"`
	Bio       string     `json:"bio"`
	Grade     int        `json:"grade"`
	Location  string     `json:"location"`
	School    string     `json:"school"`
	Work      string     `json:"work"`
	Badges    UserBadges `json:"badges"`
	CreatedAt int64      `json:"created_at"`
	UpdatedAt int64      `json:"updated_at"`
	IsOnline  bool       `json:"is_online"` // NOUVEAU : Statut de présence
}
