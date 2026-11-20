package api

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupRoutes_ArticlesEndpointsAndNoPathParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r)

	routes := r.Routes()
	require.NotEmpty(t, routes)

	expectedArticleRoutes := map[string]bool{
		"POST /articles/list":               false,
		"POST /articles/get":                false,
		"POST /admin/articles/media/upload": false,
		"POST /admin/articles/upload":       false,
		"POST /admin/articles/create":       false,
		"POST /admin/articles/set":          false,
		"PUT /admin/articles/update":        false,
		"DELETE /admin/articles/delete":     false,
		"POST /admin/articles/list":         false,
		"POST /admin/articles/get":          false,
	}

	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		if _, exists := expectedArticleRoutes[key]; exists {
			expectedArticleRoutes[key] = true
		}
		if strings.Contains(rt.Path, "article") {
			assert.NotContains(t, rt.Path, ":", "article route %s must not contain URL path parameters", rt.Path)
		}
	}

	for routeKey, found := range expectedArticleRoutes {
		assert.True(t, found, "expected article route %s to be registered in SetupRoutes", routeKey)
	}
}
