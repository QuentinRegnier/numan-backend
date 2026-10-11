package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// -------------------------------------------------------------------------
// WRAPPER POUR INTERCEPTER LA RÉPONSE
// -------------------------------------------------------------------------

// responseBodyWriter permet de capturer le corps de la réponse pour le signer
// avant qu'il ne soit envoyé au client.
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write capture les données dans le buffer au lieu de les envoyer direct
func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b) // On stocke en mémoire
	return len(b), nil
}

// -------------------------------------------------------------------------
// MIDDLEWARE
// -------------------------------------------------------------------------

func HMACMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// =====================================================================
		// PARTIE 1 : VÉRIFICATION DE LA REQUÊTE (ENTRANTE)
		// =====================================================================

		clientTs := c.GetHeader("X-Timestamp")
		clientSig := c.GetHeader("X-Signature")

		if clientTs == "" || clientSig == "" {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("MISSING_HEADERS", "Headers de sécurité manquants.", nil))
			c.Abort()
			return
		}

		userIDRaw, existsUID := c.Get("userID")
		firebaseInstallationIDRaw, existsDev := c.Get("firebaseInstallationID")

		if !existsUID || !existsDev {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("MISSING_CONTEXT", "Contexte d'authentification manquant.", nil))
			c.Abort()
			return
		}

		var userID int64

		switch v := userIDRaw.(type) {
		case float64:
			userID = int64(v)
		case string:
			p, err := pkg.ParseInt64Strict(v)
			if err == nil {
				userID = p
			}
		case int64:
			userID = v
		default:
			numan_log.Error(c).Msgf("Type userID inconnu: %T", v)
		}

		firebaseInstallationID := fmt.Sprintf("%v", firebaseInstallationIDRaw)

		var session auth_models.SessionsPayload
		var sessionFound = false

		// A. Essai Cache L1
		session, err := cache_service.LoadSessionFromCache(c, userID, firebaseInstallationID, "")
		if err == nil && session.ID != 0 {
			sessionFound = true
		} else {
			// Optionnel : On peut logger en mode debug pour ne pas spammer la prod
			numan_log.Debug(c).Err(err).Int64("user_id", userID).Msg("Cache L1 Miss (Session)")
		}

		if !sessionFound {
			// B. Essai Mongo L2
			session, errMongo := mongo.MongoLoadSession(c, userID, firebaseInstallationID, "", "")
			if errMongo == nil && session.ID != 0 {
				numan_log.Debug(c).Msg("Session trouvée dans Mongo L2, réhydratation L1...")
				sessionFound = true
				err = cache_service.SetSessionInCache(c, session)
				if err != nil {
					numan_log.Warn(c).Err(err).Int64("session_id", session.ID).Msg("Warning: Echec update Session Cache L1")
				}
			}
		}

		if !sessionFound {
			// C. Essai Postgres L3
			session, errPg := postgres.FuncLoadSession(c, -1, userID, firebaseInstallationID, "")
			if errPg == nil && session.ID != 0 {
				numan_log.Debug(c).Msg("Session trouvée dans Postgres L3, réhydratation massive...")
				sessionFound = true
				err = cache_service.SetSessionInCache(c, session)
				if err != nil {
					numan_log.Warn(c).Err(err).Int64("session_id", session.ID).Msg("Warning: Echec update Session Cache L1")
				}
				err = redis.EnqueueDB(c, session.ID, 0, redis.EntitySession, redis.ActionCreate, session, redis.TargetMongo)
				if err != nil {
					numan_log.Warn(c).Err(err).Int64("session_id", session.ID).Msg("Warning: Echec update Session Redis")
				}
			}
		}

		if !sessionFound {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("INVALID_SESSION", "Session invalide ou expirée.", nil))
			c.Abort()
			return
		}

		// 4. Anti-Rejeu (Timestamp)
		tsInt, err := pkg.ParseInt64Strict(clientTs)
		if err != nil {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("INVALID_TIMESTAMP", "Timestamp invalide.", err))
			c.Abort()
			return
		}
		now := time.Now().Unix()
		if math.Abs(float64(now-tsInt)) > float64(variables.ToleranceTimeSeconds) {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("REQUEST_EXPIRED", "Requête expirée.", nil))
			c.Abort()
			return
		}

		// 5. Lecture et Validation HMAC Requête
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, err = io.ReadAll(c.Request.Body)
			if err != nil {
				numan_error.RespondWithError(c, numan_error.NewBadRequest("READ_BODY_ERROR", "Erreur de lecture du body.", err))
				c.Abort()
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		contentToSign := security.GetBodyToSign(c.Request, bodyBytes)
		stringToSignReq := security.BuildStringToSign(c.Request.Method, c.Request.URL.Path, clientTs, contentToSign)

		usedSecret := session.CurrentSecret
		isValid := security.CheckHMAC(stringToSignReq, session.CurrentSecret, clientSig)

		if !isValid && session.LastSecret != "" {
			if session.ToleranceTime > 0 && time.Now().Before(domain.MillisToTime(session.ToleranceTime)) {
				if security.CheckHMAC(stringToSignReq, session.LastSecret, clientSig) {
					isValid = true
					usedSecret = session.LastSecret
				}
			}
		}

		if !isValid {
			numan_error.RespondWithError(c, numan_error.NewUnauthorized("INVALID_HMAC", "Signature HMAC invalide.", nil))
			c.Abort()
			return
		}

		// =====================================================================
		// PARTIE 2 : INTERCEPTION ET SIGNATURE DE LA RÉPONSE (SORTANTE)
		// =====================================================================

		w := &responseBodyWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = w

		c.Next()

		responseBody := w.body.Bytes()
		respTs := fmt.Sprintf("%d", time.Now().Unix())

		stringToSignResp := security.BuildStringToSign(
			c.Request.Method,
			c.Request.URL.Path,
			respTs,
			string(responseBody),
		)

		h := hmac.New(sha256.New, []byte(usedSecret))
		h.Write([]byte(stringToSignResp))
		respSig := hex.EncodeToString(h.Sum(nil))

		w.ResponseWriter.Header().Set("X-Timestamp", respTs)
		w.ResponseWriter.Header().Set("X-Signature", respSig)

		_, err = w.ResponseWriter.Write(responseBody)
		if err != nil {
			numan_log.Error(c).Err(err).Msg("Erreur en écrivant la réponse signée HMAC")
			return
		}
	}
}
