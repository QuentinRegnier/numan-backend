package middleware

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

const (
	ContextKeyTrustScore      = "trust_score"
	ContextKeyForceQuarantine = "force_quarantine"
)

// TrustScoreMiddleware vérifie le Trust Score de l'utilisateur en O(1) RAM :
// - Bloque immédiatement les comptes à 0.00 (Hard Ban) ou bannis stéganographiquement.
// - Positionne le flag "force_quarantine" = true si TrustScore < 0.40 (Zone de Quarantaine).
func TrustScoreMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		callerID, err := pkg.GetUserIDFromContext(c)
		if err != nil {
			numan_error.RespondWithError(c, err)
			return
		}

		// 1. Vérification O(1) du bannissement stéganographique
		if redis.SteganoBanlist != nil && redis.SteganoBanlist.Client != nil {
			isSteganoBanned, errBan := redis.SteganoBanlist.Client.SIsMember(
				c.Request.Context(),
				redis.SteganoBanlist.Key("global"),
				fmt.Sprintf("%d", callerID),
			).Result()
			if errBan == nil && isSteganoBanned {
				numan_error.RespondWithError(c, numan_error.NewForbidden(
					numan_error.CodeSteganoBanned,
					"Votre compte et votre identifiant multimédia ont été définitivement bannis.",
					nil,
				))
				return
			}
		}

		// 2. Récupération du profil allégé (Cascade L1 -> L2 -> L3)
		userLite, errLite := cache_service.GetUserLite(c.Request.Context(), callerID)
		if errLite != nil {
			numan_error.RespondWithError(c, errLite)
			return
		}

		// 3. Verrouillage Hard Ban (Score == 0.00)
		if userLite.TrustScore <= variables.TrustScoreHardBan {
			numan_error.RespondWithError(c, numan_error.NewForbidden(
				numan_error.CodeUserBanned,
				"Votre compte est verrouillé suite à un score de confiance nul (Hard Ban).",
				nil,
			))
			return
		}

		// 4. Injection dans le contexte Gin pour les routes de publication (/media/upload, /posts/set)
		isQuarantined := userLite.TrustScore < variables.TrustThresholdStandard
		c.Set(ContextKeyTrustScore, userLite.TrustScore)
		c.Set(ContextKeyForceQuarantine, isQuarantined)

		c.Next()
	}
}
