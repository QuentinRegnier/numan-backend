package auth_models

type UserPayload struct { // CreateUser
	ID               int64 `bson:"id" json:"id"`
	Birthdate        int64 `bson:"birthdate" json:"birthdate"`
	ProfilePictureID int64 `bson:"profile_picture_id" json:"profile_picture_id"`
	BanExpiresAt     int64 `bson:"ban_expires_at" json:"ban_expires_at"`
	CreatedAt        int64 `bson:"created_at" json:"created_at"`
	UpdatedAt        int64 `bson:"updated_at" json:"updated_at"`

	Username     string `bson:"username" json:"username"`
	Email        string `bson:"email" json:"email"`
	Phone        string `bson:"phone" json:"phone"`
	PasswordHash string `bson:"password_hash" json:"-"`
	FirstName    string `bson:"first_name" json:"first_name"`
	LastName     string `bson:"last_name" json:"last_name"`
	Bio          string `bson:"bio" json:"bio"`
	Location     string `bson:"location" json:"location"`
	School       string `bson:"school" json:"school"`
	Work         string `bson:"work" json:"work"`
	BanReason    string `bson:"ban_reason" json:"ban_reason"`

	Badges UserBadges `bson:"badges" json:"badges"`

	Sex   int `bson:"sex" json:"sex"`
	Grade int `bson:"grade" json:"grade"`

	EmailVerified bool `bson:"email_verified" json:"-"`
	PhoneVerified bool `bson:"phone_verified" json:"-"`
	Desactivated  bool `bson:"desactivated" json:"desactivated"`
	Banned        bool `bson:"banned" json:"banned"`
}
