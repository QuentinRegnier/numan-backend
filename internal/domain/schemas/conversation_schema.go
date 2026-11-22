package schemas

import "reflect"

// ConversationsCache
var ConversationsSchema = map[string]reflect.Kind{
	"id":              reflect.Int64,
	"type":            reflect.Int,
	"title":           reflect.String,
	"description":     reflect.String,
	"avatar_id":       reflect.Int64,
	"last_message_id": reflect.Int64,
	"state":           reflect.Int,
	"settings":        reflect.Map, // Remplacement de "laws": reflect.Slice
	"external_link":   reflect.Map,
	"lang":            reflect.String,
	"is_partner":      reflect.Bool,
	"pin_slot":        reflect.Int,
	"created_at":      reflect.Struct,
	"updated_at":      reflect.Struct,
}
