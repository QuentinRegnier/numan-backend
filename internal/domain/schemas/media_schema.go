package schemas

import "reflect"

// MediaCache
var MediaSchema = map[string]reflect.Kind{
	"id":           reflect.Int64,
	"owner_id":     reflect.Int64,
	"storage_path": reflect.String,
	"visibility":   reflect.Int,
	"ia_score":     reflect.Float64,
	"created_at":   reflect.Struct,
	"updated_at":   reflect.Struct,
}
