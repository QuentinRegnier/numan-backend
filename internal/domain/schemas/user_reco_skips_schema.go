package schemas

import "reflect"

// UserRecoSkipsSchema représente la structure de la collection MongoDB "user_reco_skips" (L2 Warm Storage)
var UserRecoSkipsSchema = map[string]reflect.Kind{
	"user_id":         reflect.Int64,
	"skipped_user_id": reflect.Int64,
	"created_at":      reflect.Struct, // Date (time.Time) pour l'index TTL MongoDB (604800s = 7 jours)
}
