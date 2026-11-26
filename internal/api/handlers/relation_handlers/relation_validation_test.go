package relation_handlers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/relation_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelationHandlers_UnActionWithoutActionField(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	gin.SetMode(gin.TestMode)

	endpoints := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"UnFollow", relation_handlers.UnFollowHandler},
		{"UnFriend", relation_handlers.UnFriendHandler},
		{"UnBlock", relation_handlers.UnBlockHandler},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+" accepts missing action field (server-enforced false)", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("userID", int64(100))
			c.Request = httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"target_id": 200}`))
			c.Request.Header.Set("Content-Type", "application/json")

			ep.handler(c)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run(ep.name+" ignores forged action=true from client", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("userID", int64(100))
			c.Request = httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"target_id": 200, "action": true}`))
			c.Request.Header.Set("Content-Type", "application/json")

			ep.handler(c)
			assert.Equal(t, http.StatusOK, w.Code)

			// Idempotent un-action on nonexistent relation must NOT enqueue any DB mutation
			queueKeys, err := repoRedis.ScanKeys(c.Request.Context(), "request_cache:q:*", 200)
			require.NoError(t, err)
			assert.Empty(t, queueKeys)
		})
	}
}
