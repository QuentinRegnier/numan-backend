package security_handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/security_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// RefreshJWT godoc
// @Summary      Renouveler le JWT (Ratchet Rotation)
// @Description  Fournit un nouveau JWT à durée de vie courte et procède à l'avancement mathématique de la clé de session (Ratchet Rotation).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Route publique disposant de sa propre logique de sécurité cryptographique.
// @Description  - Requiert l'ancien JWT (sans validation temporelle) et le dernier Secret de Session connu.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun corps JSON précis imposé, mais le corps brut est lu pour la signature.
// @Description  - Headers: `Authorization` (Bearer), `X-Secret` (Le secret côté client), `X-Signature`, `X-Timestamp`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction Sécurisée** : Récupération du corps de requête (brut) et des quatre en-têtes obligatoires. Rejet 400 si un seul manque.
// @Description  2. **Parsing JWT Dégradé** : Lecture "Unverified" du token fourni via `jwt.ParseUnverified`. Le JWT est utilisé uniquement comme transport de claims (subject et device ID), son expiration est ignorée ici.
// @Description  3. **Vérification Claims** : Rejet immédiat si le subject (UserID) ou le champ `dev` (FirebaseInstallationID) manquent ou sont in-parsables.
// @Description  4. **Validation HMAC** : Concaténation de l'URL, Timestamp et Body, signée par le serveur avec le `X-Secret` fourni. Si la signature diverge de `X-Signature`, rejet 403.
// @Description  5. **Génération JWT** : Création du nouveau token JWT via `pkg.GenerateToken`.
// @Description  6. **Rotation du Ratchet** : Exécution de `security.RotateRatchet` : le serveur calcule cryptographiquement le secret N+1, met à jour le cache de session en RAM (L1) et conserve le secret N en tolérance.
// @Description  7. **Construction de la réponse** : La réponse JSON (qui contient le nouveau JWT) est signée à l'aide de l'**ancien** secret, permettant au client de valider le serveur avant de faire tourner son propre Ratchet localement.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `security_models.RefreshJWTResponse` (contenant le nouveau Token JWT).
// @Description  - Headers: `X-Signature`, `X-Timestamp` renvoyés avec la signature sortante.
// @Description  - Persistence guarantees: Rotation synchronisée en Cache L1, persistance asynchrone DB.
// @Description  - Side effects: Invalidation irréversible des secrets ayant plus d'un cran de retard (Forward Secrecy).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[READ_BODY_ERROR] / [MISSING_HEADERS]:**
// @Description    - Trigger: Échec de lecture du corps ou absence partielle des en-têtes `X-*`.
// @Description    - Execution stage: Étape 1 & 2 (Validation initiale).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Headers de sécurité manquants.").
// @Description    - Error code: `MISSING_HEADERS` etc. ou constantes associées.
// @Description
// @Description  - **[INVALID_JWT] / [INVALID_CLAIMS] / [MISSING_SUBJECT] / [INVALID_SUBJECT] / [MISSING_DEVICE_ID]:**
// @Description    - Trigger: Le JWT est malformé, illisible, ne contient pas le UserID (`sub`) ou le `dev` ID.
// @Description    - Execution stage: Parsing Unverified (Étape 3).
// @Description    - Response: `numan_error.PublicErrorResponse` adaptée à l'absence du claim.
// @Description    - Error code: Code d'erreur spécifique ou équivalent public.
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[INVALID_HMAC] Falsification de requête:**
// @Description    - Trigger: Le HMAC fourni ne valide pas le corps de requête à l'aide du `X-Secret` donné.
// @Description    - Execution stage: Vérification HMAC (Étape 4).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Signature HMAC invalide.").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec interne de rotation:**
// @Description    - Trigger: `pkg.GenerateToken` ou `security.RotateRatchet` plantent (cache L1 inaccessible par ex).
// @Description    - Execution stage: Génération de clé / Mise à jour état L1 (Étape 5 & 6).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         security
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <Last_JWT>"
// @Param        X-Secret      header string true "Current session secret"
// @Param        X-Signature   header string true "HMAC Signature"
// @Param        X-Timestamp   header string true "Unix Timestamp"
// @Success      200  {object} security_models.RefreshJWTResponse "Successfully renewed JWT"
// @Failure      400  {object} numan_error.PublicErrorResponse "Invalid Request"
// @Failure      403  {object} numan_error.PublicErrorResponse "Invalid HMAC Signature"
// @Failure      500  {object} numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /refresh/jwt [post]
func RefreshJWT(c *gin.Context) {

	// ── ÉTAPE 1 : LECTURE DU CORPS DE REQUÊTE ──────────────────────────────

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("READ_BODY_ERROR", "Erreur lecture body.", err))
		return
	}

	// ── ÉTAPE 2 : EXTRACTION DES EN-TÊTES DE SÉCURITÉ ──────────────────────

	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		authHeader = authHeader[7:]
	}
	clientSecret := c.GetHeader("X-Secret")
	clientHMAC := c.GetHeader("X-Signature")
	clientTs := c.GetHeader("X-Timestamp")

	if authHeader == "" || clientSecret == "" || clientHMAC == "" || clientTs == "" {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("MISSING_HEADERS", "Headers de sécurité manquants.", nil))
		return
	}

	// ── ÉTAPE 3 : EXTRACTION DES DONNÉES DU JWT (MODE DÉGRADÉ) ─────────────

	// Parsing "Unverified" : on utilise l'ancien JWT uniquement comme transport de claims
	token, _, err := new(jwt.Parser).ParseUnverified(authHeader, jwt.MapClaims{})
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("INVALID_JWT", "Token illisible.", err))
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("INVALID_CLAIMS", "Claims JWT invalides.", nil))
		return
	}

	sub, err := claims.GetSubject()
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("MISSING_SUBJECT", "UserID manquant dans le token.", err))
		return
	}

	userID, err := pkg.ParseInt64Strict(sub)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("INVALID_SUBJECT", "Format UserID invalide.", err))
		return
	}

	firebaseInstallationID, ok := claims["dev"].(string)
	if !ok || firebaseInstallationID == "" {
		numan_error.RespondWithError(c, numan_error.NewBadRequest("MISSING_DEVICE_ID", "FirebaseInstallationID manquant dans le token.", nil))
		return
	}

	// ── ÉTAPE 4 : VÉRIFICATION CRYPTOGRAPHIQUE (HMAC) ──────────────────────

	contentToSign := security.GetBodyToSign(c.Request, bodyBytes)
	stringToSign := security.BuildStringToSign(c.Request.Method, c.Request.URL.Path, clientTs, contentToSign)

	if !security.CheckHMAC(stringToSign, clientSecret, clientHMAC) {
		numan_error.RespondWithError(c, numan_error.NewForbidden("INVALID_HMAC", "Signature HMAC invalide.", nil))
		return
	}

	// ── ÉTAPE 5 : GÉNÉRATION DU NOUVEAU JWT ET ROTATION DU RATCHET ─────────

	newJWT, err := pkg.GenerateToken(userID, firebaseInstallationID, variables.JWTExpirationSeconds)
	if err != nil {
		numan_log.Error(c).Err(err).Msg("Échec de la génération du JWT")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	// RotateRatchet effectue l'avancement mathématique de la clé de session et gère la persistance
	if err := security.RotateRatchet(c, userID, firebaseInstallationID, clientSecret, authHeader); err != nil {
		numan_log.Error(c).Err(err).Msg("Échec de la rotation cryptographique (RotateRatchet)")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	// ── ÉTAPE 6 : CONSTRUCTION ET SIGNATURE DE LA RÉPONSE ──────────────────

	respData := security_models.RefreshJWTResponse{
		Token:   newJWT,
		Message: "Renouvellement OK",
	}

	respBytes, err := json.Marshal(respData)
	if err != nil {
		numan_log.Error(c).Err(err).Msg("Échec de la sérialisation JSON de la réponse")
		numan_error.RespondWithError(c, numan_error.NewInternal())
		return
	}

	respTs := fmt.Sprintf("%d", domain.TimeToSeconds(domain.NowTime()))
	stringToSignResp := security.BuildStringToSign(c.Request.Method, c.Request.URL.Path, respTs, string(respBytes))

	// La réponse est signée avec le secret fourni par le client pour prouver l'authenticité du serveur
	h := hmac.New(sha256.New, []byte(clientSecret))
	h.Write([]byte(stringToSignResp))
	respSig := hex.EncodeToString(h.Sum(nil))

	c.Header("X-Timestamp", respTs)
	c.Header("X-Signature", respSig)
	c.Data(http.StatusOK, "application/json", respBytes)
}
