package schemas

import "reflect"

// ArticlesSchema représente la structure de la collection MongoDB "content.articles" (L2 Warm Storage)
var ArticlesSchema = map[string]reflect.Kind{
	"id":                   reflect.Int64,
	"title":                reflect.String,
	"summary":              reflect.String,
	"cover_media_id":       reflect.Int64,
	"markdown_media_id":    reflect.Int64,
	"attachment_media_ids": reflect.Slice,
	"partner_url":          reflect.String,
	"visibility":           reflect.Int,
	"expires_at":           reflect.Int64,
	"created_at":           reflect.Int64,
	"updated_at":           reflect.Int64,
}
