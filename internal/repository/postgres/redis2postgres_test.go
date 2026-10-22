package postgres

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedis2Postgres(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		entity         redis.EntityType
		expectedSchema string
		expectedTable  string
	}{
		{redis.EntityUser, "auth", "users"},
		{redis.EntityUserSettings, "auth", "user_settings"},
		{redis.EntitySession, "auth", "sessions"},
		{redis.EntityRelation, "auth", "relations"},
		{redis.EntityPost, "content", "posts"},
		{redis.EntityComment, "content", "comments"},
		{redis.EntityLike, "content", "likes"},
		{redis.EntityMedia, "content", "media"},
		{redis.EntityConversation, "messaging", "conversations"},
		{redis.EntityMembers, "messaging", "members"},
		{redis.EntityMessage, "messaging", "messages"},
		{redis.EntityPasskey, "auth", "user_passkeys"},
	}

	for _, tt := range tests {
		t.Run(string(tt.entity), func(t *testing.T) {
			target, err := redis2Postgres(ctx, tt.entity)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedSchema, target.Schema)
			assert.Equal(t, tt.expectedTable, target.Table)
		})
	}

	t.Run("returns_database_error_for_unsupported_entity", func(t *testing.T) {
		target, err := redis2Postgres(ctx, redis.EntityType("unsupported_entity"))
		assert.Empty(t, target.Schema)
		assert.Empty(t, target.Table)
		assertDatabaseError(t, err)
	})
}
