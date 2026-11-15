package map_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOIReviewHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	require.NoError(t, object_cache_service.SetPOIInObjectCache(t.Context(), map_models.POIPayload{
		ID:        5001,
		Type:      1,
		Title:     "Tour Eiffel",
		Latitude:  48.858370,
		Longitude: 2.294481,
		Address:   "Paris",
	}))
	require.NoError(t, redis.UsersLite.SetObject(t.Context(), int64(2001), lite_models.UserLiteRequest{ID: 2001, Username: "quentin"}))
	require.NoError(t, redis.UsersLite.SetObject(t.Context(), int64(2002), lite_models.UserLiteRequest{ID: 2002, Username: "claire"}))

	setupRouter := func(callerID int64) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if callerID > 0 {
				c.Set("userID", callerID)
			}
			c.Next()
		})
		r.POST("/map/poi/review/set", SetPOIReviewHandler)
		r.POST("/map/poi/review/get", GetPOIReviewsHandler)
		r.DELETE("/map/poi/review/delete", DeletePOIReviewHandler)
		return r
	}

	// 1. User 2001 sends pure rating (empty content) -> 200 OK
	setBody1, _ := json.Marshal(map_models.CreatePOIReviewInput{
		POIID:   5001,
		Rating:  5,
		Content: "",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/map/poi/review/set", bytes.NewReader(setBody1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	setupRouter(2001).ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	// 2. User 2002 sends review with text -> 200 OK
	setBody2, _ := json.Marshal(map_models.CreatePOIReviewInput{
		POIID:   5001,
		Rating:  4,
		Content: "Vue incroyable au coucher du soleil",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/map/poi/review/set", bytes.NewReader(setBody2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	setupRouter(2002).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var outSet2 map_models.CreatePOIReviewOutput
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &outSet2))
	assert.InDelta(t, 4.5, outSet2.POIRating, 0.0001)
	assert.Equal(t, 2, outSet2.POICommentCount)

	// 3. POST /map/poi/review/get -> returns ONLY text review (2002), ignores pure rating (2001)
	getBody, _ := json.Marshal(map_models.GetPOIReviewsInput{POIID: 5001, Limit: 20, Offset: 0})
	reqGet := httptest.NewRequest(http.MethodPost, "/map/poi/review/get", bytes.NewReader(getBody))
	reqGet.Header.Set("Content-Type", "application/json")
	wGet := httptest.NewRecorder()
	setupRouter(2001).ServeHTTP(wGet, reqGet)
	require.Equal(t, http.StatusOK, wGet.Code)

	var outGet map_models.GetPOIReviewsOutput
	require.NoError(t, json.Unmarshal(wGet.Body.Bytes(), &outGet))
	require.Len(t, outGet.Reviews, 1)
	assert.Equal(t, int64(2002), outGet.Reviews[0].UserID)
	assert.Equal(t, "Vue incroyable au coucher du soleil", outGet.Reviews[0].Content)

	// 4. DELETE /map/poi/review/delete -> removes user 2001's rating -> 200 OK
	delBody, _ := json.Marshal(map_models.DeletePOIReviewInput{POIID: 5001})
	reqDel := httptest.NewRequest(http.MethodDelete, "/map/poi/review/delete", bytes.NewReader(delBody))
	reqDel.Header.Set("Content-Type", "application/json")
	wDel := httptest.NewRecorder()
	setupRouter(2001).ServeHTTP(wDel, reqDel)
	require.Equal(t, http.StatusOK, wDel.Code)

	var outDel map_models.DeletePOIReviewOutput
	require.NoError(t, json.Unmarshal(wDel.Body.Bytes(), &outDel))
	assert.True(t, outDel.Deleted)
	assert.InDelta(t, 4.0, outDel.POIRating, 0.0001)
	assert.Equal(t, 1, outDel.POICommentCount)

	// 5. Validation & auth error checks
	badRatingBody, _ := json.Marshal(map[string]any{"poi_id": 5001, "rating": 7})
	reqBad := httptest.NewRequest(http.MethodPost, "/map/poi/review/set", bytes.NewReader(badRatingBody))
	reqBad.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	setupRouter(2001).ServeHTTP(wBad, reqBad)
	assert.Equal(t, http.StatusBadRequest, wBad.Code)

	reqUnauth := httptest.NewRequest(http.MethodPost, "/map/poi/review/set", bytes.NewReader(setBody1))
	reqUnauth.Header.Set("Content-Type", "application/json")
	wUnauth := httptest.NewRecorder()
	setupRouter(0).ServeHTTP(wUnauth, reqUnauth)
	assert.Equal(t, http.StatusUnauthorized, wUnauth.Code)

	reqDelMissing := httptest.NewRequest(http.MethodDelete, "/map/poi/review/delete", bytes.NewReader(delBody))
	reqDelMissing.Header.Set("Content-Type", "application/json")
	wDelMissing := httptest.NewRecorder()
	setupRouter(2001).ServeHTTP(wDelMissing, reqDelMissing)
	assert.Equal(t, http.StatusNotFound, wDelMissing.Code)
}
