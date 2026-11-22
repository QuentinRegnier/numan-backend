package admin_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/cluster_service"
	"github.com/gin-gonic/gin"
)

// GetClusterTopologyHandler godoc
// @Summary      Get Datacenter Cluster Topology
// @Description  Retourne la topologie temps réel des nœuds actifs enregistrés dans Redis L1 pour le datacenter courant (ou celui passé via ?datacenter=). Protégé par JWT + HMAC + AdminGuard (WireGuard).
// @Tags         admin
// @Produce      json
// @Param        datacenter query string false "Identifiant du datacenter cible (par défaut DATACENTER_ID)"
// @Success      200 {object} cluster_service.ClusterTopologyResponse
// @Router       /admin/cluster/topology [get]
func GetClusterTopologyHandler(c *gin.Context) {
	dc := c.Query("datacenter")
	topology, appErr := cluster_service.GetDatacenterTopology(c.Request.Context(), dc)
	if appErr != nil {
		numan_error.RespondWithError(c, appErr)
		return
	}
	c.JSON(http.StatusOK, topology)
}
