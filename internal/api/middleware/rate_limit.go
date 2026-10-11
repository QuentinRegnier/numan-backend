package middleware

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/gin-gonic/gin"
)

const (
	// Limite : 50 requêtes maximum
	rateLimitMaxRequests = 50
)

// RateLimiter empêche les attaques DDoS applicatives
func RateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// On identifie l'utilisateur par son IP
		clientIP := c.ClientIP()

		ctx := context.Background()

		// Utilisation propre de la Collection RateLimits (DDD)
		count, err := redis.RateLimits.Incr(ctx, clientIP)
		if err != nil {
			// Fail-Open : on laisse passer si Redis plante pour ne pas bloquer l'API
			c.Next()
			return
		}

		// Si c'est la première requête, on rafraîchit le TTL (10s préconfiguré dans le manager)
		if count == 1 {
			err = redis.RateLimits.RefreshTTL(ctx, clientIP)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Str("client_ip", clientIP).Msg("Impossible de rafraîchir le TTL du rate limit")
				c.Next()
				return
			}
		}

		// Si on dépasse la limite
		if count > rateLimitMaxRequests {
			errLimit := numan_error.NewAppError(http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "Too many requests. Please calm down.", nil)
			numan_error.RespondWithError(c, errLimit)
			c.Abort()
			return
		}

		c.Next()
	}
}
