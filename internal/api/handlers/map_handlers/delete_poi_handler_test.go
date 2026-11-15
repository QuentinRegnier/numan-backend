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

func TestDeletePOIHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	require.NoError(t, object_cache_service.SetPOIInObjectCache(t.Context(), map_models.POIPayload{
		ID:        901,
		Type:      1,
		Title:     "POI à supprimer",
		Latitude:  48.85,
		Longitude: 2.35,
	}))

	r := gin.New()
	r.DELETE("/admin/map/poi/delete", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		DeletePOIHandler(c)
	})

	body, _ := json.Marshal(map_models.DeletePOIInput{
		POIIDs: []int64{901},
	})
	req := httptest.NewRequest(http.MethodDelete, "/admin/map/poi/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out map_models.DeletePOIOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 1, out.DeletedCount)
	assert.Equal(t, []int64{901}, out.POIIDs)
}
