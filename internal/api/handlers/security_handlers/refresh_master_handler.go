package security_handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/security_models"
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

// RefreshMaster godoc
// @Summary      Actualiser le MasterToken et rotation de sécurité
// @Description  Remplace la clé d'accès maître (MasterToken), réinitialise le ratchet cryptographique associé à la session et délivre un nouveau JWT.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Route publique non bloquée par le middleware JWT classique.
// @Description  - Authentification spécifique reposant sur un MasterToken existant (`input.MasterToken`) et une signature HMAC de la requête.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `MasterToken`, `UserID`, `Username` passés dans le corps (JSON `security_models.RefreshMasterInput`).
// @Description  - Headers: `X-Signature` (HMAC calculé sur body), `X-Timestamp` obligatoires. `Authorization` (Optionnel).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction de sécurité** : Validation basique du payload (GIN `Unmarshal` + `ValidateStruct`) et des headers `X-Signature`/`X-Timestamp`. Rejet immédiat 400 si absents.
// @Description  2. **Recherche de Session en Cascade** : Tentative de récupération de la session active avec auto-guérison :
// @Description     - Cache RAM L1 (Prioritaire).
// @Description     - Base MongoDB L2 (Si absent en L1, avec re-hydratation synchrone L1).
// @Description     - Base Postgres L3 (Si absent L2, avec Write-Behind vers L2 et ré-hydratation L1).
// @Description  3. **Vérification HMAC** : Le corps de la requête concaténé au timestamp doit avoir une signature identique au HMAC envoyé, signé à l'aide de l'**ancien** `MasterToken`. Rejet 403 Forbidden si altéré.
// @Description  4. **Génération des jetons** : Production d'un nouveau `MasterToken` (longue durée) et `JWT` (courte durée) via `pkg.GenerateToken`.
// @Description  5. **Réinitialisation Cryptographique** : Le secret rotatif (Ratchet) est remis à 0 (`security.ResetRatchet`), invalidant les secrets de session éphémères antérieurs.
// @Description  6. **Mise à jour Session** : Les nouvelles valeurs, y compris la tolérance de temps, sont écrites en mémoire (L1) et envoyées en persistance asynchrone (Write-Behind L2+L3).
// @Description  7. **Signature de la réponse** : La réponse JSON (qui contient les nouveaux jetons en clair) est elle-même signée avec **l'ancien** MasterToken, permettant au client de valider son authenticité.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `security_models.RefreshMasterResponse` contenant les nouveaux `MasterToken` et `Token` (JWT).
// @Description  - Headers: `X-Signature`, `X-Timestamp`.
// @Description  - Persistence guarantees: Mise à jour synchrone Cache L1, file d'attente Redis pour DB.
// @Description  - Side effects: Invalidations effectives des ratchets et JWT précédents (au-delà de la tolérance).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[READ_BODY_ERROR] / [EMPTY_BODY] / [INVALID_PAYLOAD] / [VALIDATION_FAILED]:**
// @Description    - Trigger: Le body est illisible, vide, JSON malformé ou rate la validation structurale.
// @Description    - Execution stage: `io.ReadAll` ou Validation `pkg.ValidateStruct`.
// @Description    - Response: `numan_error.PublicErrorResponse` avec cause détaillée.
// @Description    - Error code: Contexte de l'erreur converti vers `numan_error.CodeInvalidPayload`.
// @Description
// @Description  - **[MISSING_HEADERS]:**
// @Description    - Trigger: Les en-têtes `X-Signature` ou `X-Timestamp` n'ont pas été fournis.
// @Description    - Execution stage: Extraction des headers (Étape 1).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Headers de sécurité manquants.").
// @Description    - Error code: String "MISSING_HEADERS" ou son équivalent constant public.
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[SESSION_NOT_FOUND] Session invalide/expirée:**
// @Description    - Trigger: Le MasterToken fourni n'existe dans aucune couche (L1, L2, L3).
// @Description    - Execution stage: Cascade de recherche de session.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Session introuvable.").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  - **[INVALID_HMAC] Falsification interceptée:**
// @Description    - Trigger: La signature reconstruite avec l'ancien MasterToken ne correspond pas à `X-Signature`.
// @Description    - Execution stage: Vérification cryptographique (`security.CheckHMAC`).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Signature HMAC invalide...").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec fatal cryptographique:**
// @Description    - Trigger: La génération du token ou la réinitialisation du Ratchet échoue.
// @Description    - Execution stage: `pkg.GenerateToken` ou `security.ResetRatchet`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         security
// @Accept       json
// @Produce      json
// @Param        Authorization header string false "Bearer <Current_JWT> (Optional)"
// @Param        X-Signature   header string true  "HMAC calculated with OLD MasterToken"
// @Param        X-Timestamp   header string true  "Unix Timestamp"
// @Param        input         body   security_models.RefreshMasterInput true "Reset data (MasterToken, UserID, Username)"
// @Success      200  {object} security_models.RefreshMasterResponse "Newly generated credentials"
// @Failure      400  {object} numan_error.PublicErrorResponse "Invalid payload or Missing Headers"
// @Failure      403  {object} numan_error.PublicErrorResponse "MasterToken not found or Invalid HMAC"
// @Failure      500  {object} numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /refresh/master [post]
func RefreshMaster(c *gin.Context) {

	// ── ÉTAPE 1 : LECTURE ET VALIDATION DE LA REQUÊTE ──────────────────────

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("READ_BODY_ERROR", "Erreur de lecture du body.", err))
		return
	}

	var input security_models.RefreshMasterInput
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &input); err != nil {
			numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
			return
		}
	} else {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("EMPTY_BODY", "Le corps de la requête est requis.", nil))
		return
	}

	if err := pkg.ValidateStruct(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("VALIDATION_FAILED", "Validation failed.", err))
		return
	}

	// ── ÉTAPE 2 : EXTRACTION DES EN-TÊTES DE SÉCURITÉ ──────────────────────

	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		authHeader = authHeader[7:]
	} else {
		authHeader = ""
	}

	clientHMAC := c.GetHeader("X-Signature")
	clientTs := c.GetHeader("X-Timestamp")

	if clientHMAC == "" || clientTs == "" {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("MISSING_HEADERS", "Headers de sécurité manquants.", nil))
		return
	}

	// ── ÉTAPE 3 : RECHERCHE DE SESSION EN CASCADE (L1 -> L2 -> L3) ─────────

	var sessionRaw auth_models.SessionsPayload
	var sessionFound bool

	// Tentative L1 (Object Cache RAM)
	if s, err := cache_service.LoadSessionFromCache(c, input.UserID, "", input.MasterToken); err == nil && s.ID != 0 {
		sessionRaw = s
		sessionFound = true
	}

	// Tentative L2 (MongoDB - Warm Storage) avec auto-guérison L1
	if !sessionFound {
		if s, err := mongo.MongoLoadSession(c, input.UserID, "", input.MasterToken, ""); err == nil && s.ID != 0 {
			sessionRaw = s
			sessionFound = true
			err = cache_service.SetSessionInCache(c, sessionRaw)
			if err != nil {
				numan_log.Warn(c).Err(err).Int64("session_id", sessionRaw.ID).Msg("Warning: Echec update Session Cache L1")
				numan_error.RespondWithError(c, numan_error.NewInternal())
				return
			}
		}
	}

	// Tentative L3 (PostgreSQL - Cold Storage) avec auto-guérison L1 & L2
	if !sessionFound {
		s, err := postgres.FuncLoadSession(c, -1, input.UserID, "", input.MasterToken)
		if err == nil && s.ID != 0 {
			sessionRaw = s
			sessionFound = true
			err = redis.EnqueueDB(c, s.ID, 0, redis.EntitySession, redis.ActionCreate, s, redis.TargetMongo)
			if err != nil {
				numan_log.Warn(c).Err(err).Int64("session_id", sessionRaw.ID).Msg("Warning: Echec update Session Redis")
			}
			err = cache_service.SetSessionInCache(c, s)
			if err != nil {
				numan_log.Warn(c).Err(err).Int64("session_id", sessionRaw.ID).Msg("Warning: Echec update Session Cache L1")
				numan_error.RespondWithError(c, numan_error.NewInternal())
				return
			}
		}
	}

	if !sessionFound || sessionRaw.ID == 0 {
		numan_error.RespondWithError(c, numan_error.NewForbidden("SESSION_NOT_FOUND", "Session introuvable.", nil))
		return
	}

	// ── ÉTAPE 4 : VÉRIFICATION CRYPTOGRAPHIQUE (HMAC) ──────────────────────

	contentToSign := security.GetBodyToSign(c.Request, bodyBytes)
	stringToSign := security.BuildStringToSign(c.Request.Method, c.Request.URL.Path, clientTs, contentToSign)

	if !security.CheckHMAC(stringToSign, input.MasterToken, clientHMAC) {
		numan_error.RespondWithError(c, numan_error.NewForbidden("INVALID_HMAC", "Signature HMAC invalide (Master Check).", nil))
		return
	}

	// ── ÉTAPE 5 : GÉNÉRATION DES NOUVEAUX JETONS & ROTATION ────────────────

	newMasterToken, err := pkg.GenerateToken(input.UserID, sessionRaw.FirebaseInstallationID, variables.MasterTokenExpirationSeconds)
	if err != nil {
		numan_log.Error(c).Err(err).Int64("user_id", input.UserID).Msg("Échec de la génération du MasterToken")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	newJWT, err := pkg.GenerateToken(input.UserID, sessionRaw.FirebaseInstallationID, variables.JWTExpirationSeconds)
	if err != nil {
		numan_log.Error(c).Err(err).Int64("user_id", input.UserID).Msg("Échec de la génération du JWT")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	if sessionRaw.CurrentSecret, err = security.ResetRatchet(newMasterToken, sessionRaw.FirebaseInstallationID); err != nil {
		numan_log.Error(c).Err(err).Int64("user_id", input.UserID).Msg("Échec de la réinitialisation du Ratchet")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	// ── ÉTAPE 6 : MISE À JOUR ET PERSISTANCE DE LA SESSION ─────────────────

	sessionRaw.MasterToken = newMasterToken
	sessionRaw.LastSecret = sessionRaw.FirebaseInstallationID
	sessionRaw.LastJWT = authHeader
	sessionRaw.ToleranceTime = domain.TimeToMillis(time.Now().Add(time.Duration(variables.ToleranceTimeSeconds) * time.Second))
	sessionRaw.ExpiresAt = domain.TimeToMillis(time.Now().Add(time.Duration(variables.MasterTokenExpirationSeconds) * time.Second))

	// Persistance L1
	if errAdd := cache_service.SetSessionInCache(c, sessionRaw); errAdd != nil {
		numan_log.Warn(c).Err(errAdd).Msg("Warning: Echec update Session Cache L1")
	}

	// Persistance Asynchrone (Write-Behind)
	if err := redis.EnqueueDB(c, sessionRaw.ID, 0, redis.EntitySession, redis.ActionUpdate, sessionRaw, redis.TargetAll); err != nil {
		numan_log.Error(c).Err(err).Msg("Error enqueuing to DB")
	}

	// ── ÉTAPE 7 : CONSTRUCTION ET SIGNATURE DE LA RÉPONSE ──────────────────

	respData := security_models.RefreshMasterResponse{
		MasterToken: newMasterToken,
		Token:       newJWT,
		Message:     "Master Reset Successful",
	}

	respBytes, err := json.Marshal(respData)
	if err != nil {
		numan_log.Error(c).Err(err).Int64("user_id", input.UserID).Msg("Échec de la sérialisation JSON de la réponse")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	respTs := fmt.Sprintf("%d", time.Now().Unix())

	stringToSignResp := security.BuildStringToSign(
		c.Request.Method,
		c.Request.URL.Path,
		respTs,
		string(respBytes),
	)

	// La réponse est signée avec l'ancien MasterToken pour validation côté client
	h := hmac.New(sha256.New, []byte(input.MasterToken))
	h.Write([]byte(stringToSignResp))
	respSig := hex.EncodeToString(h.Sum(nil))

	c.Header("X-Timestamp", respTs)
	c.Header("X-Signature", respSig)

	c.Data(http.StatusOK, "application/json", respBytes)
}
