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
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePOIHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	require.NoError(t, object_cache_service.SetPOIInObjectCache(t.Context(), map_models.POIPayload{
		ID:        801,
		Type:      1,
		Title:     "Ancien Titre",
		Latitude:  48.85,
		Longitude: 2.35,
		Rating:    4.0,
	}))

	r := gin.New()
	r.PUT("/admin/map/poi/update", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		UpdatePOIHandler(c)
	})

	newTitle := "Nouveau Titre"
	body, _ := json.Marshal(map_models.UpdatePOIInput{
		ID:    801,
		Title: &newTitle,
	})
	req := httptest.NewRequest(http.MethodPut, "/admin/map/poi/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out map_models.UpdatePOIOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 1, out.UpdatedCount)
	require.Len(t, out.POIs, 1)
	assert.Equal(t, "Nouveau Titre", out.POIs[0].Title)
}
