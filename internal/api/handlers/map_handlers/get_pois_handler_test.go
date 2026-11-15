package map_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPOIsInBBoxHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	require.NoError(t, object_cache_service.SetPOIInObjectCache(t.Context(), map_models.POIPayload{
		ID:        501,
		Type:      1,
		Title:     "Tour Eiffel",
		Latitude:  48.858370,
		Longitude: 2.294481,
		Address:   "Champ de Mars, Paris",
		Rating:    4.9,
	}))

	r := gin.New()
	r.POST("/map/poi/get", func(c *gin.Context) {
		c.Set("userID", int64(2001))
		GetPOIsInBBoxHandler(c)
	})

	// 1. Valid BBox request -> 200 OK
	body, _ := json.Marshal(map_models.GetPoisInBBoxInput{
		SWLat: 48.80,
		SWLng: 2.25,
		NELat: 48.90,
		NELng: 2.35,
	})
	req := httptest.NewRequest(http.MethodPost, "/map/poi/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out map_models.GetPoisInBBoxOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.POIs, 1)
	assert.Equal(t, int64(501), out.POIs[0].ID)

	// 2. Invalid BBox coordinates -> 400 Bad Request
	badBody, _ := json.Marshal(map_models.GetPoisInBBoxInput{
		SWLat: 49.0,
		SWLng: 2.25,
		NELat: 48.0,
		NELng: 2.35,
	})
	reqBad := httptest.NewRequest(http.MethodPost, "/map/poi/get", bytes.NewReader(badBody))
	reqBad.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqBad)
	assert.Equal(t, http.StatusBadRequest, wBad.Code)

	// 3. Unauthorized (missing userID) -> 401
	rUnauth := gin.New()
	rUnauth.POST("/map/poi/get", GetPOIsInBBoxHandler)
	reqUnauth := httptest.NewRequest(http.MethodPost, "/map/poi/get", bytes.NewReader(body))
	reqUnauth.Header.Set("Content-Type", "application/json")
	wUnauth := httptest.NewRecorder()
	rUnauth.ServeHTTP(wUnauth, reqUnauth)
	assert.Equal(t, http.StatusUnauthorized, wUnauth.Code)
}
