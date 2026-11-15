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

func TestSetPOIHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	r := gin.New()
	r.POST("/admin/map/poi/set", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		SetPOIHandler(c)
	})

	body, _ := json.Marshal(map_models.SetPOIInput{
		ID:          701,
		Type:        2,
		Title:       "Les Deux Magots",
		Description: "Café littéraire",
		Latitude:    48.8540,
		Longitude:   2.3331,
		Address:     "6 Place Saint-Germain des Prés, 75006 Paris",
		Rating:      4.5,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/map/poi/set", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out map_models.SetPOIOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 1, out.CreatedCount)
	assert.Equal(t, []int64{701}, out.POIIDs)
}
