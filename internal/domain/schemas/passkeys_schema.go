package schemas

import "reflect"

var PasskeysSchema = map[string]reflect.Kind{
	"id":               reflect.Int64,
	"user_id":          reflect.Int64,
	"credential_id":    reflect.String,
	"public_key":       reflect.Slice,
	"attestation_type": reflect.String,
	"aaguid":           reflect.Slice,
	"sign_count":       reflect.Int64,
	"device_name":      reflect.String,
	"transports":       reflect.Slice,
	"created_at":       reflect.Int64,
	"updated_at":       reflect.Int64,
}
