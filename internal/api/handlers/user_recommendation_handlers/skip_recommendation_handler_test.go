package user_recommendation_handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/user_recommendation_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkipUserRecommendationHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.UserRecoSkips = nil
	ctx := context.Background()

	callerID := int64(950)
	seedHandlerTestUser(t, ctx, callerID, "caller_950")
	seedHandlerTestUser(t, ctx, 951, "skipped_951")

	r := gin.New()
	r.POST("/users/recommendations/skips", func(c *gin.Context) {
		c.Set("userID", callerID)
		user_recommendation_handlers.SkipUserRecommendationHandler(c)
	})

	// 1. Valid skip request -> 200 OK
	validBody, _ := json.Marshal(user_recommendation_models.SkipUserRecommendationInput{
		SkippedUserID: 951,
	})
	req := httptest.NewRequest(http.MethodPost, "/users/recommendations/skips", bytes.NewReader(validBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out user_recommendation_models.SkipUserRecommendationOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "ok", out.Status)
	assert.Equal(t, int64(951), out.SkippedUserID)

	// 2. Self-skip rejected -> 400 Bad Request
	selfBody, _ := json.Marshal(user_recommendation_models.SkipUserRecommendationInput{
		SkippedUserID: callerID,
	})
	reqSelf := httptest.NewRequest(http.MethodPost, "/users/recommendations/skips", bytes.NewReader(selfBody))
	reqSelf.Header.Set("Content-Type", "application/json")
	wSelf := httptest.NewRecorder()
	r.ServeHTTP(wSelf, reqSelf)
	assert.Equal(t, http.StatusBadRequest, wSelf.Code)

	// 3. Invalid payload (skipped_user_id <= 0 or malformed JSON) -> 400 Bad Request
	reqZero := httptest.NewRequest(http.MethodPost, "/users/recommendations/skips", bytes.NewReader([]byte(`{"skipped_user_id": 0}`)))
	reqZero.Header.Set("Content-Type", "application/json")
	wZero := httptest.NewRecorder()
	r.ServeHTTP(wZero, reqZero)
	assert.Equal(t, http.StatusBadRequest, wZero.Code)

	reqMalformed := httptest.NewRequest(http.MethodPost, "/users/recommendations/skips", bytes.NewReader([]byte(`{bad_json`)))
	reqMalformed.Header.Set("Content-Type", "application/json")
	wMalformed := httptest.NewRecorder()
	r.ServeHTTP(wMalformed, reqMalformed)
	assert.Equal(t, http.StatusBadRequest, wMalformed.Code)

	// 4. Unauthorized (missing userID in context) -> 401 Unauthorized
	rUnauth := gin.New()
	rUnauth.POST("/users/recommendations/skips", user_recommendation_handlers.SkipUserRecommendationHandler)
	reqUnauth := httptest.NewRequest(http.MethodPost, "/users/recommendations/skips", bytes.NewReader(validBody))
	reqUnauth.Header.Set("Content-Type", "application/json")
	wUnauth := httptest.NewRecorder()
	rUnauth.ServeHTTP(wUnauth, reqUnauth)
	assert.Equal(t, http.StatusUnauthorized, wUnauth.Code)
}
