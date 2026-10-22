package mongo

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestRedis2Mongo(t *testing.T) {
	defer resetCollections()()

	ctx := context.Background()

	// 1. All collections nil -> returns database initialization error
	Users = nil
	UserSettings = nil
	Sessions = nil
	Relations = nil
	Posts = nil
	Comments = nil
	Likes = nil
	Media = nil
	Conversations = nil
	Members = nil
	Messages = nil
	MessageReactions = nil
	Saved = nil
	Passkeys = nil
	Notifications = nil

	supportedEntities := []redis.EntityType{
		redis.EntityUser,
		redis.EntityUserSettings,
		redis.EntitySession,
		redis.EntityRelation,
		redis.EntityPost,
		redis.EntityComment,
		redis.EntityLike,
		redis.EntityMedia,
		redis.EntityConversation,
		redis.EntityMembers,
		redis.EntityMessage,
		redis.EntityMessageReaction,
		redis.EntitySaved,
		redis.EntityPasskey,
		redis.EntityNotification,
	}

	for _, entity := range supportedEntities {
		coll, err := redis2Mongo(ctx, entity)
		assert.Nil(t, coll)
		assertDatabaseError(t, err)
	}

	// 2. Unsupported entity
	coll, err := redis2Mongo(ctx, redis.EntityType("unsupported_type"))
	assert.Nil(t, coll)
	assertDatabaseError(t, err)

	// 3. Supported entities resolve properly when initialized
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("resolves collections", func(mt *mtest.T) {
		mockColl := newMockCollection(mt, "test", nil)
		Users = mockColl
		UserSettings = mockColl
		Sessions = mockColl
		Relations = mockColl
		Posts = mockColl
		Comments = mockColl
		Likes = mockColl
		Media = mockColl
		Conversations = mockColl
		Members = mockColl
		Messages = mockColl
		MessageReactions = mockColl
		Saved = mockColl
		Passkeys = mockColl
		Notifications = mockColl

		for _, entity := range supportedEntities {
			c, err := redis2Mongo(ctx, entity)
			require.NoError(mt, err)
			assert.Equal(mt, mockColl, c)
		}
	})
}
