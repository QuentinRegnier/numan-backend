package moderation_handlers

import (
	"bytes"
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewQuarantineHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	adminUser := lite_models.UserLiteRequest{
		ID:         1,
		Username:   "admin_user",
		Grade:      variables.UserGradeAdmin,
		TrustScore: 1.0,
	}
	normalUser := lite_models.UserLiteRequest{
		ID:         2,
		Username:   "normal_user",
		Grade:      variables.UserGradeUser,
		TrustScore: 0.5,
	}
	authorUser := lite_models.UserLiteRequest{
		ID:         10,
		Username:   "author_user",
		Grade:      variables.UserGradeUser,
		TrustScore: 0.20,
	}

	require.NoError(t, redis.UsersLite.SetObject(ctx, adminUser.ID, adminUser))
	require.NoError(t, redis.UsersLite.SetObject(ctx, normalUser.ID, normalUser))
	require.NoError(t, redis.UsersLite.SetObject(ctx, authorUser.ID, authorUser))

	setupRouter := func(userID any) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if userID != nil {
				c.Set("userID", userID)
			}
			c.Next()
		})
		r.POST("/admin/moderation/quarantine/review", ReviewQuarantineHandler)
		return r
	}

	mediaCols := []string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"}
	postCols := []string{
		"id", "user_id", "content", "hashtags", "indirect_hashtags", "identifiers",
		"media_ids", "visibility", "priority_level", "location", "created_at", "updated_at",
		"like_count", "comment_count", "view_count", "report_count", "has_media",
		"vector", "vector_version", "telemetry_dwell_sum", "telemetry_dwell_sq", "telemetry_clicks",
	}

	t.Run("returns_200_and_approves_media_and_post_with_bonus_event", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "content.get_media") {
				return test.NewMockRows(mediaCols, [][]driver.Value{
					{int64(70), int64(10), "uploads/test.avif", int64(2), now, now},
				}), nil
			}
			if strings.Contains(query, "content.func_load_posts") {
				return test.NewMockRows(postCols, [][]driver.Value{
					{
						int64(500), int64(10), "Quarantine post", "{tag}", "{}", "{}",
						"{70}", 3, 0, "Paris", now, now, 0, 0, 0, 0, true,
						"{}", 1, float64(0), float64(0), 0,
					},
				}), nil
			}
			return test.NewMockRows([]string{}, nil), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":70,"post_id":500,"decision":"approve","reason":"Image conforme"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, `"media_id":70`)
		assert.Contains(t, body, `"new_visibility":1`)
		assert.Contains(t, body, `"trust_event":"QUARANTINE_APPROVED"`)
	})

	t.Run("returns_200_and_rejects_media_with_minor_infraction", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "content.get_media") {
				return test.NewMockRows(mediaCols, [][]driver.Value{
					{int64(71), int64(10), "uploads/minor.avif", int64(2), now, now},
				}), nil
			}
			return test.NewMockRows([]string{}, nil), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":71,"decision":"reject_minor","reason":"Hors sujet"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, `"media_id":71`)
		assert.Contains(t, body, `"new_visibility":-2`)
		assert.Contains(t, body, `"trust_event":"MINOR_INFRACTION"`)
	})

	t.Run("returns_200_and_rejects_media_with_major_infraction", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "content.get_media") {
				return test.NewMockRows(mediaCols, [][]driver.Value{
					{int64(72), int64(10), "uploads/major.avif", int64(2), now, now},
				}), nil
			}
			return test.NewMockRows([]string{}, nil), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":72,"decision":"reject_major","reason":"Contenu illicite"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, `"media_id":72`)
		assert.Contains(t, body, `"new_visibility":-2`)
		assert.Contains(t, body, `"trust_event":"MAJOR_INFRACTION"`)
	})

	t.Run("returns_400_when_decision_value_is_invalid", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":70,"decision":"unknown_decision"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")
	})

	t.Run("returns_400_when_media_id_is_missing", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"decision":"approve"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")
	})

	t.Run("returns_401_when_caller_is_missing_from_context", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":70,"decision":"approve"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(nil).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns_403_when_caller_is_not_a_moderator", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":70,"decision":"approve"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(normalUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "FORBIDDEN_ACCESS")
	})

	t.Run("returns_404_when_media_is_not_found", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(mediaCols, [][]driver.Value{}), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":9999,"decision":"approve"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "RESOURCE_NOT_FOUND")
	})

	t.Run("returns_500_when_postgres_is_uninitialized", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		w := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/admin/moderation/quarantine/review",
			bytes.NewBufferString(`{"media_id":70,"decision":"approve"}`),
		)
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
