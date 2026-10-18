package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// DeletePasskeyHandler godoc
// @Summary      Supprimer une Passkey
// @Description  Révoque une Passkey (clé matérielle FIDO2/WebAuthn) du compte utilisateur. Si aucune autre Passkey n'est présente après la suppression, retire automatiquement la méthode Passkey du bitmask 2FA du profil.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization`) et une signature HMAC Ratchet valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: L'utilisateur appelant doit être le propriétaire légitime de la Passkey à supprimer.
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `passkey_id` (entier, l'identifiant interne de la Passkey à révoquer).
// @Description  - Optional fields: Aucun.
// @Description  - Validation rules: Binding GIN standard sur `two_factor_models.DeletePasskeyInput`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant de l'appelant via `pkg.GetUserIDFromContext` et validation du JSON (`c.ShouldBindJSON`).
// @Description  2. Vérification d'appartenance : la Passkey (`passkey_id`) doit exister et appartenir à l'utilisateur (`userID`) via `cache_service.GetUserPasskeysCascade`.
// @Description  3. Suppression immédiate de la Passkey de toutes ses clés d'indexation dans Redis L1 (`DeletePasskeyFromObjectCache`).
// @Description  4. Persistance asynchrone Write-Behind de la suppression via `redis.EnqueueDB` (ActionDelete sur `EntityPasskey`).
// @Description  5. Si c'était la dernière Passkey de l'utilisateur, mise à jour du profil utilisateur en L1 (retrait du bitmask Passkey) et Write-Behind sur `EntityUser` (ActionUpdate).
// @Description  6. Renvoi immédiat d'un statut HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Aucun (corps vide).
// @Description  - Persistence guarantees: Purge synchrone de la Passkey en RAM L1 et suppression asynchrone dans MongoDB (L2) et PostgreSQL (L3) via `redis.EnqueueDB`.
// @Description  - Side effects: Modification potentielle du bitmask `TwoFactorMethod` du profil utilisateur si la collection de Passkeys devient vide.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON format or validation failed:**
// @Description    - Trigger: Le champ `passkey_id` est absent ou mal typé.
// @Description    - Required request/state: Corps JSON invalide.
// @Description    - Execution stage: Validation GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[UNAUTHORIZED] Caller not identified in context:**
// @Description    - Trigger: Absence d'identifiant utilisateur valide dans le contexte Gin.
// @Description    - Required request/state: Token JWT invalide ou contexte corrompu.
// @Description    - Execution stage: `pkg.GetUserIDFromContext`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `"UNAUTHORIZED"` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🔍 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Passkey not found on account:**
// @Description    - Trigger: Le `passkey_id` fourni ne correspond à aucune Passkey associée au compte de l'utilisateur (ou elle a déjà été supprimée).
// @Description    - Required request/state: L'utilisateur tente de supprimer une clé inexistante ou n'appartenant pas à son compte.
// @Description    - Execution stage: Vérification d'appartenance dans `auth_service.DeletePasskey`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Write-Behind queue or database failure:**
// @Description    - Trigger: Échec critique de `redis.EnqueueDB` ou erreur inattendue lors de la lecture des Passkeys en L3.
// @Description    - Required request/state: Indisponibilité de Redis ou PostgreSQL.
// @Description    - Execution stage: Persistance Write-Behind.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   two_factor_models.DeletePasskeyInput    true "Identifiant de la Passkey à révoquer"
// @Success      200
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden / Unauthorized"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Passkey Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/passkeys [delete]
func DeletePasskeyHandler(c *gin.Context) {
	userID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input two_factor_models.DeletePasskeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	if err := two_factor_service.DeletePasskey(c.Request.Context(), userID, input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.Status(http.StatusOK)
}
