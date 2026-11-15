package user_settings_handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/user_settings_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePrivacyHandler_EnumBindings(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	require.NoError(t, object_cache_service.SetUserSettings(ctx, user_settings_models.UserSettingsPayload{
		ID:     501,
		UserID: 500,
	}))

	t.Run("accepts upper valid enum bounds (ProfileVisibility=2, AllowTagging=3, HideConnections=3)", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", int64(500))
		body := `{"profile_visibility":2,"post_visibility_default":2,"conversation_permission":3,"add_group_permission":2,"allow_tagging":3,"allow_mentions":3,"hide_connections":3}`
		c.Request = httptest.NewRequest(http.MethodPut, "/settings/privacy", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")

		user_settings_handlers.UpdatePrivacyHandler(c)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("rejects PostVisibilityDefault=3 with 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", int64(500))
		body := `{"profile_visibility":2,"post_visibility_default":3}`
		c.Request = httptest.NewRequest(http.MethodPut, "/settings/privacy", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")

		user_settings_handlers.UpdatePrivacyHandler(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
