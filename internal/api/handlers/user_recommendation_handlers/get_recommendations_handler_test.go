package user_recommendation_handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/user_recommendation_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedHandlerTestUser(t *testing.T, ctx context.Context, id int64, username string) {
	t.Helper()
	u := auth_models.UserPayload{
		ID:        id,
		Username:  username,
		FirstName: username,
		LastName:  "HandlerTest",
		Location:  "Paris",
	}
	require.NoError(t, redis.Users.SetObject(ctx, id, u))
	s := user_settings_models.UserSettingsPayload{
		ID:     id,
		UserID: id,
		Privacy: user_settings_models.PrivacySettings{
			ShowLocation: true,
		},
		DisplayAndContent: user_settings_models.DisplayAndContentSettings{
			Language: 0,
		},
	}
	require.NoError(t, object_cache_service.SetUserSettings(ctx, s))
	require.NoError(t, cache_service.AddUserToSpeedCache(ctx, u, s))
}

func TestGetUserRecommendationsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.UserRecoSkips = nil
	ctx := context.Background()

	callerID := int64(900)
	seedHandlerTestUser(t, ctx, callerID, "caller_900")
	seedHandlerTestUser(t, ctx, 901, "recommended_901")
	seedHandlerTestUser(t, ctx, 902, "recommended_902")

	require.NoError(t, user_recommendation_service.GenerateUserRecommendations(ctx, callerID))

	r := gin.New()
	r.POST("/users/recommendations", func(c *gin.Context) {
		c.Set("userID", callerID)
		user_recommendation_handlers.GetUserRecommendationsHandler(c)
	})
	r.POST("/users/recommendations/force", func(c *gin.Context) {
		c.Set("userID", callerID)
		user_recommendation_handlers.ForceUserRecommendationsHandler(c)
	})

	// 1. Default JSON body ({} -> sort=global, limit=20, force=false) -> 200 OK, no compute lock acquired
	req := httptest.NewRequest(http.MethodPost, "/users/recommendations", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out user_recommendation_models.GetUserRecommendationsOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Users, 2)
	assert.Equal(t, int64(901), out.Users[0].User.ID)
	assert.Equal(t, int64(902), out.Users[1].User.ID)

	hasLockBeforeForce, errLock := cache_service.HasUserRecoComputeLock(ctx, callerID)
	require.NoError(t, errLock)
	assert.False(t, hasLockBeforeForce)

	// 2. Empty body on POST /users/recommendations -> 200 OK with defaults
	reqEmpty := httptest.NewRequest(http.MethodPost, "/users/recommendations", nil)
	wEmpty := httptest.NewRecorder()
	r.ServeHTTP(wEmpty, reqEmpty)
	assert.Equal(t, http.StatusOK, wEmpty.Code)

	// 3. Explicit JSON body on /users/recommendations/force (sort=geo, limit=1, force=true via route variant) -> 200 OK & lock acquired
	geoBody, _ := json.Marshal(user_recommendation_models.GetUserRecommendationsInput{
		Sort:  "geo",
		Limit: 1,
	})
	reqForceGeo := httptest.NewRequest(http.MethodPost, "/users/recommendations/force", bytes.NewReader(geoBody))
	reqForceGeo.Header.Set("Content-Type", "application/json")
	wForceGeo := httptest.NewRecorder()
	r.ServeHTTP(wForceGeo, reqForceGeo)

	assert.Equal(t, http.StatusOK, wForceGeo.Code)
	var outGeo user_recommendation_models.GetUserRecommendationsOutput
	require.NoError(t, json.Unmarshal(wForceGeo.Body.Bytes(), &outGeo))
	require.Len(t, outGeo.Users, 1)

	hasLockAfterForce, errLockAfter := cache_service.HasUserRecoComputeLock(ctx, callerID)
	require.NoError(t, errLockAfter)
	assert.True(t, hasLockAfterForce)

	// 4. Invalid sort parameter in JSON body -> 400 Bad Request
	reqBadSort := httptest.NewRequest(http.MethodPost, "/users/recommendations", bytes.NewReader([]byte(`{"sort":"invalid","limit":10}`)))
	reqBadSort.Header.Set("Content-Type", "application/json")
	wBadSort := httptest.NewRecorder()
	r.ServeHTTP(wBadSort, reqBadSort)
	assert.Equal(t, http.StatusBadRequest, wBadSort.Code)

	// 5. Invalid limit parameter in JSON body (-5, 51, non-integer, malformed JSON) -> 400 Bad Request
	for _, badPayload := range []string{
		`{"sort":"global","limit":-5}`,
		`{"sort":"global","limit":51}`,
		`{"sort":"global","limit":"abc"}`,
		`{malformed_json`,
	} {
		reqBadLimit := httptest.NewRequest(http.MethodPost, "/users/recommendations", bytes.NewReader([]byte(badPayload)))
		reqBadLimit.Header.Set("Content-Type", "application/json")
		wBadLimit := httptest.NewRecorder()
		r.ServeHTTP(wBadLimit, reqBadLimit)
		assert.Equal(t, http.StatusBadRequest, wBadLimit.Code)
	}

	// 6. Unauthorized (missing userID in context) on both routes -> 401 Unauthorized
	rUnauth := gin.New()
	rUnauth.POST("/users/recommendations", user_recommendation_handlers.GetUserRecommendationsHandler)
	rUnauth.POST("/users/recommendations/force", user_recommendation_handlers.ForceUserRecommendationsHandler)

	for _, path := range []string{"/users/recommendations", "/users/recommendations/force"} {
		reqUnauth := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
		reqUnauth.Header.Set("Content-Type", "application/json")
		wUnauth := httptest.NewRecorder()
		rUnauth.ServeHTTP(wUnauth, reqUnauth)
		assert.Equal(t, http.StatusUnauthorized, wUnauth.Code)
	}
}
