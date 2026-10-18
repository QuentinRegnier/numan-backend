package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// GetPasskeysHandler godoc
// @Summary      Lister les Passkeys enregistrées de l'utilisateur
// @Description  Récupère la liste des clés matérielles FIDO2 / WebAuthn associées au compte de l'utilisateur connecté via la cascade L1 (Redis) -> L2 (MongoDB) -> L3 (PostgreSQL).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization`) et une signature HMAC Ratchet valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: L'utilisateur appelant ne peut consulter que ses propres Passkeys.
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun paramètre de requête ni corps JSON (l'identité est extraite du JWT).
// @Description  - Optional fields: Aucun.
// @Description  - Validation rules: Vérification de présence du `userID` dans le contexte Gin.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant de l'utilisateur appelant via `pkg.GetUserIDFromContext`.
// @Description  2. Recherche des Passkeys en cascade via `object_cache_service.GetUserPasskeysCascade` :
// @Description     - Tentative L1 dans Redis (`redis.Passkeys`).
// @Description     - En cas de Cache Miss, tentative L2 dans MongoDB (`mongo.MongoLoadUserPasskeys`) et réhydratation L1.
// @Description     - En cas de Cache Miss L2, lecture L3 dans PostgreSQL (`postgres.FuncGetUserPasskeys`), réhydratation synchrone L1 et promotion asynchrone L2 via `redis.EnqueueDB`.
// @Description  3. Projection des `PasskeyPayload` vers le DTO sécurisé `PasskeyView` (exclusion physique de la clé publique brute et de l'AAGUID).
// @Description  4. Renvoi de la liste JSON au client.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `two_factor_models.GetPasskeysOutput` contenant un tableau `passkeys` (toujours initialisé à `[]` si vide).
// @Description  - Persistence guarantees: Lecture seule avec auto-guérison des caches L1/L2 en cas de Cache Miss.
// @Description  - Side effects: Réhydratation de `redis.Passkeys` (L1) et `auth.user_passkeys` (MongoDB L2) si les données proviennent de PostgreSQL (L3).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[UNAUTHORIZED] Caller not identified in context:**
// @Description    - Trigger: Absence d'identifiant utilisateur dans le contexte Gin.
// @Description    - Required request/state: Contexte d'authentification manquant.
// @Description    - Execution stage: `pkg.GetUserIDFromContext`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `"UNAUTHORIZED"` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] PostgreSQL L3 query failure:**
// @Description    - Trigger: Échec de connexion ou d'exécution SQL lors du fallback sur `auth.func_get_user_passkeys`.
// @Description    - Required request/state: Cache Miss L1/L2 combiné à une panne PostgreSQL L3.
// @Description    - Execution stage: `object_cache_service.GetUserPasskeysCascade`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Success      200  {object}  two_factor_models.GetPasskeysOutput
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden / Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/passkeys [get]
func GetPasskeysHandler(c *gin.Context) {
	userID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	output, err := two_factor_service.GetUserPasskeys(c.Request.Context(), userID)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
