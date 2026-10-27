package schemas

import "reflect"

// PostsCache
var PostsSchema = map[string]reflect.Kind{
	"id":                  reflect.Int64,
	"user_id":             reflect.Int64,
	"content":             reflect.String,
	"hashtags":            reflect.Slice, // Text[]
	"indirect_hashtags":   reflect.Slice, // ✅ NOUVEAU : Text[]
	"identifiers":         reflect.Slice, // Bigint[]
	"media_ids":           reflect.Slice, // Bigint[]
	"visibility":          reflect.Int,
	"priority_level":      reflect.Int,
	"location":            reflect.String,
	"like_count":          reflect.Int,
	"comment_count":       reflect.Int,
	"view_count":          reflect.Int,
	"report_count":        reflect.Int,
	"has_media":           reflect.Bool,
	"ia_score_max":        reflect.Float64,
	"vector":              reflect.Slice,   // REAL[] -> []float32
	"vector_version":      reflect.Int,     // Integer
	"telemetry_dwell_sum": reflect.Float64, // Somme des temps de lecture
	"telemetry_dwell_sq":  reflect.Float64, // Somme des carrés (pour la variance)
	"telemetry_clicks":    reflect.Int,     // Compteur d'interactions directes
	"created_at":          reflect.Struct,
	"updated_at":          reflect.Struct,
}
