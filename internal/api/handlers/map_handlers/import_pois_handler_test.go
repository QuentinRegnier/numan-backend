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
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportPOIsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	r := gin.New()
	r.POST("/admin/map/poi/import", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		ImportPOIsHandler(c)
	})

	body, _ := json.Marshal(map_models.ImportPOIsInput{
		POIs: []map_models.CreatePOIItemInput{
			{
				ID:          601,
				Type:        1,
				Title:       "Musée Rodin",
				Description: "Hôtel Biron",
				Latitude:    48.8553,
				Longitude:   2.3158,
				Address:     "77 Rue de Varenne, 75007 Paris",
				Rating:      4.8,
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/map/poi/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out map_models.ImportPOIsOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 1, out.ImportedCount)
	assert.Equal(t, []int64{601}, out.POIIDs)

	// Non-admin user_grade -> 403 Forbidden
	rNonAdmin := gin.New()
	rNonAdmin.POST("/admin/map/poi/import", func(c *gin.Context) {
		c.Set("userID", int64(4002))
		c.Set("user_grade", 0)
		ImportPOIsHandler(c)
	})
	reqForbidden := httptest.NewRequest(http.MethodPost, "/admin/map/poi/import", bytes.NewReader(body))
	reqForbidden.Header.Set("Content-Type", "application/json")
	wForbidden := httptest.NewRecorder()
	rNonAdmin.ServeHTTP(wForbidden, reqForbidden)
	assert.Equal(t, http.StatusForbidden, wForbidden.Code)
}
