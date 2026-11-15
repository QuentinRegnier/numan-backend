package search_handlers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/search_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupSearchRouter(userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID > 0 {
			c.Set("userID", userID)
		}
		c.Next()
	})
	r.POST("/search/autocomplete/tags", search_handlers.AutocompleteTagHandler)
	r.POST("/search/autocomplete/text", search_handlers.AutocompleteTextHandler)
	r.POST("/search/post", search_handlers.SearchPostHandler)
	return r
}

func TestSearchHandlers_ValidationAndOffsetZeroCompatibility(t *testing.T) {
	_ = test.SetupTestRedis(t)
	r := setupSearchRouter(42)

	// Offset=0 must be accepted (previously failed binding:"required")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/search/autocomplete/tags", bytes.NewBufferString(`{"query":"golang","offset":0,"limit":15}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Filter-only SearchPost with empty query & offset=0 must be accepted
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/search/post", bytes.NewBufferString(`{"query":"","filter":"likes","offset":0,"limit":10}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Whitespace-only query and missing filter rejected with 400
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/search/post", bytes.NewBufferString(`{"query":"   ","offset":0,"limit":10}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Whitespace-only autocomplete query rejected with 400
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/search/autocomplete/text", bytes.NewBufferString(`{"query":"   ","offset":0,"limit":10}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
