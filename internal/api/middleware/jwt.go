package middleware

import (
	"os"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("MISSING_TOKEN", "Authorization header manquant.", nil))
			c.Abort()
			return
		}

		if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
			tokenString = tokenString[7:]
		}

		keyFunc := func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, numan_error.NewUnauthorized("INVALID_SIGNING_ALGORITHM", "Algorithme JWT invalide.", nil)
			}
			return []byte(os.Getenv("JWT_SECRET")), nil
		}

		token, err := jwt.Parse(tokenString, keyFunc)
		if err != nil || !token.Valid {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("INVALID_TOKEN", "Token invalide.", err))
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || !token.Valid {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("INVALID_CLAIMS", "Claims invalides.", nil))
			c.Abort()
			return
		}

		if exp, ok := claims["exp"].(float64); ok {
			if domain.NowTime().After(time.Unix(int64(exp), 0)) {
				numan_error.RespondWithError(c, numan_error.NewUnauthorized("EXPIRED_TOKEN", "Token expiré.", nil))
				c.Abort()
				return
			}
		}

		// EXTRACTION DES DONNÉES CLÉS
		c.Set("userID", claims["sub"]) // ID de l'utilisateur

		if dev, ok := claims["dev"].(string); ok {
			c.Set("firebaseInstallationID", dev) // Identifiant unique de la session
		} else {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("OBSOLETE_TOKEN", "Token format obsolete (missing device info).", nil))
			c.Abort()
			return
		}

		c.Next()
	}
}
