package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSetupRoutes_NoPublicTokenBackdoor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	api.SetupRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "public /token route must not exist")
}

func TestRoleRouting_WebSocketOnlyExposesWS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	api.SetupCommonMiddlewares(r)
	api.SetupWebSocketRoutes(r)

	// /ws doit exister (retourne 401 sans JWT, jamais 404)
	wWS := httptest.NewRecorder()
	r.ServeHTTP(wWS, httptest.NewRequest(http.MethodGet, "/ws", nil))
	assert.NotEqual(t, http.StatusNotFound, wWS.Code, "/ws must be mounted on ws_node")

	// Les routes REST publiques et admin ne doivent PAS exister sur un ws_node
	wLogin := httptest.NewRecorder()
	r.ServeHTTP(wLogin, httptest.NewRequest(http.MethodPost, "/login", nil))
	assert.Equal(t, http.StatusNotFound, wLogin.Code, "/login must not be mounted on ws_node")

	wTopo := httptest.NewRecorder()
	r.ServeHTTP(wTopo, httptest.NewRequest(http.MethodGet, "/admin/cluster/topology", nil))
	assert.Equal(t, http.StatusNotFound, wTopo.Code, "/admin/cluster/topology must not be mounted on ws_node")
}

func TestRoleRouting_RESTOnlyExcludesWSAndExposesClusterTopology(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	api.SetupCommonMiddlewares(r)
	api.SetupRESTRoutes(r)

	// /ws ne doit PAS être monté sur un api_node pur
	wWS := httptest.NewRecorder()
	r.ServeHTTP(wWS, httptest.NewRequest(http.MethodGet, "/ws", nil))
	assert.Equal(t, http.StatusNotFound, wWS.Code, "/ws must not be mounted on api_node")

	// /admin/cluster/topology doit être monté et protégé (401 sans JWT, jamais 404)
	wTopo := httptest.NewRecorder()
	r.ServeHTTP(wTopo, httptest.NewRequest(http.MethodGet, "/admin/cluster/topology", nil))
	assert.Equal(t, http.StatusUnauthorized, wTopo.Code, "/admin/cluster/topology must be mounted and JWT-protected")
}
