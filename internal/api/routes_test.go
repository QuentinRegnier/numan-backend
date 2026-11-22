package api

import (
	"net/http"
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

func TestSetupRoutes_CommunityRecommendationEndpointsAndNoPathParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupRoutes(r)

	expected := map[string]bool{
		http.MethodPost + " /communities/recommendations": false,
		http.MethodPost + " /admin/communities/pin":       false,
		http.MethodPost + " /admin/communities/unpin":     false,
		http.MethodPost + " /admin/communities/partner":   false,
	}

	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
			assert.False(t, strings.Contains(route.Path, ":"), "route %s must not contain URL path params", route.Path)
			assert.False(t, strings.Contains(route.Path, "*"), "route %s must not contain wildcard params", route.Path)
		}
	}

	for routeKey, found := range expected {
		assert.True(t, found, "expected route %s to be registered", routeKey)
	}
}
