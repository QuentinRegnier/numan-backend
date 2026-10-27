package middleware

import (
	"net"
	"os"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// AdminGuard vérifie que l'utilisateur est admin ET vient du VPN WireGuard.
func AdminGuard() gin.HandlerFunc {
	// Plage IP de ton réseau WireGuard (ex: défini dans .env)
	wireguardCIDR := os.Getenv("WIREGUARD_SUBNET")
	if wireguardCIDR == "" {
		wireguardCIDR = "10.8.0.0/24" // Fallback ou sécurité à définir
	}

	// Précompilation du CIDR au démarrage pour éviter de le parser à chaque requête
	_, wgNet, err := net.ParseCIDR(wireguardCIDR)
	if err != nil {
		panic("CIDR WireGuard invalide configuré pour le middleware Admin: " + err.Error())
	}

	return func(c *gin.Context) {
		// 1. VÉRIFICATION RÉSEAU (WireGuard)
		clientIP := net.ParseIP(c.ClientIP())
		if clientIP == nil || !wgNet.Contains(clientIP) {
			// On renvoie un 404 au lieu d'un 403 pour masquer l'existence même de la route
			c.AbortWithStatusJSON(404, numan_error.PublicErrorResponse{
				Error:   "Not Found",
				Code:    numan_error.CodeNotFound,
				TraceID: c.GetString("trace_id"),
			})
			return
		}

		// 2. VÉRIFICATION APPLICATIVE (Rôle Admin)
		userID, err := pkg.GetUserIDFromContext(c)
		if err != nil || userID == 0 {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("", "Non authentifié", err))
			return
		}

		userLite, err := cache_service.GetUserLite(c.Request.Context(), userID)
		if err != nil {
			numan_error.RespondWithError(c, numan_error.NewInternal(err))
			return
		}

		// Grade Administrateur (4 ou fallback 1)
		if userLite.Grade != variables.UserGradeAdmin && userLite.Grade != variables.MemberRoleAdmin {
			numan_error.RespondWithError(c, numan_error.NewForbidden("", "Privilèges insuffisants.", nil))
			return
		}

		c.Next()
	}
}

// ModeratorGuard vérifie que l'utilisateur possède au moins le rang Modérateur (Grade >= 3 ou Admin).
func ModeratorGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := pkg.GetUserIDFromContext(c)
		if err != nil || userID == 0 {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("", "Non authentifié", err))
			return
		}

		userLite, err := cache_service.GetUserLite(c.Request.Context(), userID)
		if err != nil {
			numan_error.RespondWithError(c, numan_error.NewInternal(err))
			return
		}

		if userLite.Grade < variables.UserGradeModerator && userLite.Grade != variables.MemberRoleAdmin {
			numan_error.RespondWithError(c, numan_error.NewForbidden("", "Privilèges de modération requis.", nil))
			return
		}

		c.Set("user_grade", userLite.Grade)
		c.Next()
	}
}
