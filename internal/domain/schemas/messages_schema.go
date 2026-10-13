package schemas

import "reflect"

// MessagesCache
var MessagesSchema = map[string]reflect.Kind{
	"id":                  reflect.Int64,
	"conversation_id":     reflect.Int64,
	"sender_id":           reflect.Int64,
	"message_type":        reflect.Int,
	"state":               reflect.Int,
	"content":             reflect.String,
	"attachments":         reflect.Map, // JSONB
	"allow_new_reactions": reflect.Bool,
	"created_at":          reflect.Struct,
	"updated_at":          reflect.Struct,
}
