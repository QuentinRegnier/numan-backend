package test

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

// resetCollections saves and returns a restore function for all global collections.
func ResetCollections() func() {
	origUsers := mongo.Users
	origUserSettings := mongo.UserSettings
	origSessions := mongo.Sessions
	origRelations := mongo.Relations
	origPosts := mongo.Posts
	origComments := mongo.Comments
	origLikes := mongo.Likes
	origMedia := mongo.Media
	origConversations := mongo.Conversations
	origMembers := mongo.Members
	origMessages := mongo.Messages
	origMessageReactions := mongo.MessageReactions
	origSaved := mongo.Saved
	origPasskeys := mongo.Passkeys
	origNotifications := mongo.Notifications

	return func() {
		mongo.Users = origUsers
		mongo.UserSettings = origUserSettings
		mongo.Sessions = origSessions
		mongo.Relations = origRelations
		mongo.Posts = origPosts
		mongo.Comments = origComments
		mongo.Likes = origLikes
		mongo.Media = origMedia
		mongo.Conversations = origConversations
		mongo.Members = origMembers
		mongo.Messages = origMessages
		mongo.MessageReactions = origMessageReactions
		mongo.Saved = origSaved
		mongo.Passkeys = origPasskeys
		mongo.Notifications = origNotifications
	}
}

func NewMockCollection(mt *mtest.T, name string, schema map[string]reflect.Kind) *mongo.Collection {
	return &mongo.Collection{
		Name:   name,
		Schema: schema,
		DB:     mt.DB,
	}
}

func AssertDatabaseError_Mongo(t testing.TB, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.True(t, errors.As(err, &appErr), "expected *numan_error.AppError, got %T: %v", err, err)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
	assert.NotEmpty(t, appErr.Message)
	assert.NotNil(t, appErr.Err, "underlying cause must not be nil")
}

func AssertNotFoundError_Mongo(t testing.TB, err error, expectedCode string) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.True(t, errors.As(err, &appErr), "expected *numan_error.AppError, got %T: %v", err, err)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	if expectedCode != "" {
		assert.Equal(t, expectedCode, appErr.Code)
	}
	assert.NotEmpty(t, appErr.Message)
	assert.NotNil(t, appErr.Err, "underlying cause must not be nil for not found")
}
