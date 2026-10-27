package mongo_test

import (
	"context"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestRedis2Mongo(t *testing.T) {
	defer test.ResetCollections()

	ctx := context.Background()

	// 1. All collections nil -> returns database initialization error
	mongo.Users = nil
	mongo.UserSettings = nil
	mongo.Sessions = nil
	mongo.Relations = nil
	mongo.Posts = nil
	mongo.Comments = nil
	mongo.Likes = nil
	mongo.Media = nil
	mongo.Conversations = nil
	mongo.Members = nil
	mongo.Messages = nil
	mongo.MessageReactions = nil
	mongo.Saved = nil
	mongo.Passkeys = nil
	mongo.Notifications = nil

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
		coll, err := mongo.Redis2MongoForTest(ctx, entity)
		assert.Nil(t, coll)
		test.AssertDatabaseError_Mongo(t, err)
	}

	// 2. Unsupported entity
	coll, err := mongo.Redis2MongoForTest(ctx, redis.EntityType("unsupported_type"))
	assert.Nil(t, coll)
	test.AssertDatabaseError_Mongo(t, err)

	// 3. Supported entities resolve properly when initialized
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("resolves collections", func(mt *mtest.T) {
		mockColl := test.NewMockCollection(mt, "test", nil)
		mongo.Users = mockColl
		mongo.UserSettings = mockColl
		mongo.Sessions = mockColl
		mongo.Relations = mockColl
		mongo.Posts = mockColl
		mongo.Comments = mockColl
		mongo.Likes = mockColl
		mongo.Media = mockColl
		mongo.Conversations = mockColl
		mongo.Members = mockColl
		mongo.Messages = mockColl
		mongo.MessageReactions = mockColl
		mongo.Saved = mockColl
		mongo.Passkeys = mockColl
		mongo.Notifications = mockColl

		for _, entity := range supportedEntities {
			c, err := mongo.Redis2MongoForTest(ctx, entity)
			require.NoError(mt, err)
			assert.Equal(mt, mockColl, c)
		}
	})
}
