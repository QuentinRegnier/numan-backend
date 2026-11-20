package article_handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectQueryParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("accepts request without query string", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/articles/create", nil)

		require.NoError(t, rejectQueryParameters(c))
	})

	t.Run("rejects request with query string", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/articles/create?status=1", nil)

		err := rejectQueryParameters(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "query")
	})

	t.Run("handles nil request safely", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = nil

		require.NoError(t, rejectQueryParameters(c))
	})
}
