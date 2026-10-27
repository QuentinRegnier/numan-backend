package moderation_handlers

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
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

func TestGetModerationQueueHandler(t *testing.T) {
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
	quarantineAuthor := lite_models.UserLiteRequest{
		ID:         10,
		Username:   "young_author",
		Grade:      variables.UserGradeUser,
		TrustScore: 0.20,
	}

	require.NoError(t, redis.UsersLite.SetObject(ctx, adminUser.ID, adminUser))
	require.NoError(t, redis.UsersLite.SetObject(ctx, normalUser.ID, normalUser))
	require.NoError(t, redis.UsersLite.SetObject(ctx, quarantineAuthor.ID, quarantineAuthor))

	setupRouter := func(userID any) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if userID != nil {
				c.Set("userID", userID)
			}
			c.Next()
		})
		r.POST("/admin/moderation/queue", GetModerationQueueHandler)
		return r
	}

	mediaCols := []string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"}
	reportCols := []string{
		"id", "reporter_id", "report_origin", "target_type", "target_ids",
		"target_owner_id", "author_trust_score", "ia_score_max", "accumulated_report_score", "report_count",
		"category", "reason", "rationale", "state", "importance", "created_at", "updated_at",
	}

	t.Run("returns_200_and_quarantined_items_with_reports_and_stats_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "moderation.func_load_quarantined_media_paginated") {
				return test.NewMockRows(mediaCols, [][]driver.Value{
					{int64(70), int64(10), "uploads/quarantine.avif", int64(2), now, now},
				}), nil
			}
			if strings.Contains(query, "moderation.func_load_pending_reports_paginated") {
				return test.NewMockRows(reportCols, [][]driver.Value{
					{int64(501), int64(0), 1, 0, "{100}", int64(10), float64(0.20), float64(0.88), float64(0.0), 1, 10, "Auto AI NSFW", "", 0, float64(2.32), now, now},
				}), nil
			}
			return test.NewMockRows([]string{}, nil), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", bytes.NewBufferString(`{"limit":10,"offset":0,"origin_filter":1}`))
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, `"media_id":70`)
		assert.Contains(t, body, `"owner_id":10`)
		assert.Contains(t, body, `"owner_trust_score":0.2`)
		assert.Contains(t, body, `"id":501`)
		assert.Contains(t, body, `"report_origin_label":"auto_ai"`)
		assert.Contains(t, body, `"auto_ai_reports_count":1`)
	})

	t.Run("returns_200_with_empty_queue_when_no_records_exist", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "moderation.func_load_quarantined_media_paginated") {
				return test.NewMockRows(mediaCols, [][]driver.Value{}), nil
			}
			if strings.Contains(query, "moderation.func_load_pending_reports_paginated") {
				return test.NewMockRows(reportCols, [][]driver.Value{}), nil
			}
			return test.NewMockRows([]string{}, nil), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", nil)
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"quarantined_media":[]`)
		assert.Contains(t, w.Body.String(), `"pending_reports":[]`)
	})

	t.Run("returns_400_when_json_payload_is_malformed", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", bytes.NewBufferString(`{invalid-json}`))
		req.Header.Set("Content-Type", "application/json")
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")
	})

	t.Run("returns_401_when_caller_is_missing_from_context", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", nil)
		setupRouter(nil).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("returns_403_when_caller_is_not_a_moderator_or_admin", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", nil)
		setupRouter(normalUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "FORBIDDEN_ACCESS")
	})

	t.Run("returns_500_when_database_fails_to_load_quarantined_media", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "moderation.func_load_quarantined_media_paginated") {
				return nil, errors.New("database connection down")
			}
			return test.NewMockRows([]string{}, nil), nil
		}, nil)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", nil)
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "DATABASE_ERROR")
	})

	t.Run("returns_500_when_postgres_is_uninitialized", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", nil)
		setupRouter(adminUser.ID).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
