package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// DisableTwoFactorHandler godoc
// @Summary      Désactiver une méthode 2FA spécifique ou l'intégralité de la 2FA
// @Description  Retire une méthode spécifique (`1`: Email, `2`: TOTP, `3`: Passkey) du bitmask 2FA de l'utilisateur ou désactive complètement la double authentification (`0`) après vérification du mot de passe.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization`) et une signature HMAC Ratchet valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: L'utilisateur appelant doit fournir son `password_hash` valide.
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `password_hash` (chaîne non vide).
// @Description  - Optional fields: `method` (entier `0` pour tout désactiver, `1` pour Email, `2` pour TOTP, `3` pour Passkey).
// @Description  - Validation rules: Binding GIN sur `two_factor_models.DisableTwoFactorInput`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant de l'appelant (`pkg.GetUserIDFromContext`) et validation du JSON (`c.ShouldBindJSON`).
// @Description  2. Chargement du profil utilisateur via la cascade L1 -> L2 -> L3 (`loadUserByIDCascade`).
// @Description  3. Vérification de concordance du `password_hash` fourni avec celui de l'utilisateur.
// @Description  4. Mise à jour du bitmask `TwoFactorMethod` (et purge de `TOTPSecret` si `method == 0` ou `method == 2`). Si plus aucun bit n'est actif, `TwoFactorEnabled` passe à `false`.
// @Description  5. Mise à jour synchrone du cache L1 (`cache_service.SetUserInCache`) et persistance asynchrone Write-Behind (`redis.EnqueueDB` sur `EntityUser`).
// @Description  6. Renvoi immédiat d'un statut HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Aucun (corps vide).
// @Description  - Persistence guarantees: Mise à jour synchrone en RAM L1 et persistance asynchrone dans MongoDB (L2) et PostgreSQL (L3) via `redis.EnqueueDB`.
// @Description  - Side effects: Le bitmask 2FA de l'utilisateur est mis à jour immédiatement pour les prochaines connexions.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON or method out of range:**
// @Description    - Trigger: `password_hash` manquant ou `method` hors de `[0, 1, 2, 3]`.
// @Description    - Required request/state: Corps JSON invalide.
// @Description    - Execution stage: Validation GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Incorrect password:**
// @Description    - Trigger: Le `password_hash` fourni ne correspond pas à celui enregistré sur le compte.
// @Description    - Required request/state: Mauvais mot de passe.
// @Description    - Execution stage: Vérification du mot de passe dans `auth_service.DisableTwoFactor`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Database or Write-Behind failure:**
// @Description    - Trigger: Échec de lecture L3 ou d'enfilement dans `redis.EnqueueDB`.
// @Description    - Required request/state: Indisponibilité de Redis ou PostgreSQL.
// @Description    - Execution stage: `auth_service.DisableTwoFactor`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                   true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                   true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                   true "Timestamp Unix de la requête"
// @Param        input         body   two_factor_models.DisableTwoFactorInput  true "Mot de passe et méthode à désactiver"
// @Success      200
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Incorrect Password"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/2fa/disable [post]
func DisableTwoFactorHandler(c *gin.Context) {
	userID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input two_factor_models.DisableTwoFactorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	if err := two_factor_service.DisableTwoFactor(c.Request.Context(), userID, input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.Status(http.StatusOK)
}
