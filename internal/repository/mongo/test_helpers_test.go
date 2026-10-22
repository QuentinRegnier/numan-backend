package mongo

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

// resetCollections saves and returns a restore function for all global collections.
func resetCollections() func() {
	origUsers := Users
	origUserSettings := UserSettings
	origSessions := Sessions
	origRelations := Relations
	origPosts := Posts
	origComments := Comments
	origLikes := Likes
	origMedia := Media
	origConversations := Conversations
	origMembers := Members
	origMessages := Messages
	origMessageReactions := MessageReactions
	origSaved := Saved
	origPasskeys := Passkeys
	origNotifications := Notifications

	return func() {
		Users = origUsers
		UserSettings = origUserSettings
		Sessions = origSessions
		Relations = origRelations
		Posts = origPosts
		Comments = origComments
		Likes = origLikes
		Media = origMedia
		Conversations = origConversations
		Members = origMembers
		Messages = origMessages
		MessageReactions = origMessageReactions
		Saved = origSaved
		Passkeys = origPasskeys
		Notifications = origNotifications
	}
}

func newMockCollection(mt *mtest.T, name string, schema map[string]reflect.Kind) *Collection {
	return &Collection{
		Name:   name,
		Schema: schema,
		DB:     mt.DB,
	}
}

func assertDatabaseError(t testing.TB, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.True(t, errors.As(err, &appErr), "expected *numan_error.AppError, got %T: %v", err, err)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
	assert.NotEmpty(t, appErr.Message)
	assert.NotNil(t, appErr.Err, "underlying cause must not be nil")
}

func assertNotFoundError(t testing.TB, err error, expectedCode string) {
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
