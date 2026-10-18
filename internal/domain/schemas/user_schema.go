package schemas

import "reflect"

// UsersCache représente la structure du cache_service "users"
var UsersSchema = map[string]reflect.Kind{
	"id":                 reflect.Int64, // UUID
	"username":           reflect.String,
	"email":              reflect.String,
	"email_verified":     reflect.Bool,
	"phone":              reflect.String,
	"phone_verified":     reflect.Bool,
	"password_hash":      reflect.String,
	"two_factor_enabled": reflect.Bool,
	"two_factor_method":  reflect.Int,
	"totp_secret":        reflect.String,
	"first_name":         reflect.String,
	"last_name":          reflect.String,
	"birthdate":          reflect.Struct, // Date au format string
	"sex":                reflect.Int,
	"bio":                reflect.String,
	"profile_picture_id": reflect.Int64, // UUID
	"grade":              reflect.Int,
	"location":           reflect.String,
	"school":             reflect.String,
	"work":               reflect.String,
	"badges":             reflect.Map, // JSONB
	"desactivated":       reflect.Bool,
	"banned":             reflect.Bool,
	"ban_reason":         reflect.String,
	"ban_expires_at":     reflect.Struct, // Timestamp au format string
	"created_at":         reflect.Struct,
	"updated_at":         reflect.Struct,
}
