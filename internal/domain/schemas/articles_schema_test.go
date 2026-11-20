package schemas_test

import (
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/schemas"
	"github.com/stretchr/testify/assert"
)

func TestArticlesSchema(t *testing.T) {
	assert.Equal(t, reflect.Int64, schemas.ArticlesSchema["id"])
	assert.Equal(t, reflect.String, schemas.ArticlesSchema["title"])
	assert.Equal(t, reflect.String, schemas.ArticlesSchema["summary"])
	assert.Equal(t, reflect.Int64, schemas.ArticlesSchema["cover_media_id"])
	assert.Equal(t, reflect.Int64, schemas.ArticlesSchema["markdown_media_id"])
	assert.Equal(t, reflect.Slice, schemas.ArticlesSchema["attachment_media_ids"])
	assert.Equal(t, reflect.String, schemas.ArticlesSchema["partner_url"])
	assert.Equal(t, reflect.Int, schemas.ArticlesSchema["visibility"])
	assert.Equal(t, reflect.Int64, schemas.ArticlesSchema["expires_at"])
	assert.Equal(t, reflect.Int64, schemas.ArticlesSchema["created_at"])
	assert.Equal(t, reflect.Int64, schemas.ArticlesSchema["updated_at"])
}
