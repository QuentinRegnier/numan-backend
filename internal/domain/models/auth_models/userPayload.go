package auth_models

type UserPayload struct {
	Badges           UserBadges `bson:"badges" json:"badges"`
	Username         string     `bson:"username" json:"username"`
	Email            string     `bson:"email" json:"email"`
	Phone            string     `bson:"phone" json:"phone"`
	PasswordHash     string     `bson:"password_hash" json:"password_hash"`
	TOTPSecret       string     `bson:"totp_secret" json:"totp_secret" msgpack:"totp_secret"`
	FirstName        string     `bson:"first_name" json:"first_name"`
	LastName         string     `bson:"last_name" json:"last_name"`
	Bio              string     `bson:"bio" json:"bio"`
	Location         string     `bson:"location" json:"location"`
	School           string     `bson:"school" json:"school"`
	Work             string     `bson:"work" json:"work"`
	BanReason        string     `bson:"ban_reason" json:"ban_reason"`
	ID               int64      `bson:"id" json:"id"`
	Birthdate        int64      `bson:"birthdate" json:"birthdate"`
	ProfilePictureID int64      `bson:"profile_picture_id" json:"profile_picture_id"`
	BanExpiresAt     int64      `bson:"ban_expires_at" json:"ban_expires_at"`
	CreatedAt        int64      `bson:"created_at" json:"created_at"`
	UpdatedAt        int64      `bson:"updated_at" json:"updated_at"`
	TwoFactorMethod  int        `bson:"two_factor_method" json:"two_factor_method" msgpack:"two_factor_method"`
	Sex              int        `bson:"sex" json:"sex"`
	Grade            int        `bson:"grade" json:"grade"`
	TwoFactorEnabled bool       `bson:"two_factor_enabled" json:"two_factor_enabled" msgpack:"two_factor_enabled"`
	EmailVerified    bool       `bson:"email_verified" json:"email_verified"`
	PhoneVerified    bool       `bson:"phone_verified" json:"phone_verified"`
	Desactivated     bool       `bson:"desactivated" json:"desactivated"`
	Banned           bool       `bson:"banned" json:"banned"`
}
