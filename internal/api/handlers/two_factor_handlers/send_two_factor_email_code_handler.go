package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// SendTwoFactorEmailCodeHandler godoc
// @Summary      Envoyer ou renvoyer un code OTP 2FA par Email
// @Description  Génère un nouveau code OTP à 6 chiffres, met à jour son empreinte HMAC-SHA256 dans la `PreAuthSession` en RAM L1 et déclenche l'expédition asynchrone de l'email via le Worker SMTP. Permet à un utilisateur ayant plusieurs méthodes 2FA configurées (ex: Passkey + Email) de demander l'envoi du code Email à la volée lors de la connexion, ou de demander le renvoi d'un nouveau code.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (Route publique protégée par `pre_auth_token` éphémère).
// @Description  - Required permissions or roles: Possession d'un `pre_auth_token` valide autorisant la méthode Email (`1`).
// @Description  - Relevant middleware: MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `pre_auth_token` (chaîne non vide).
// @Description  - Optional fields: Aucun.
// @Description  - Validation rules: Binding GIN standard sur `two_factor_models.SendTwoFactorEmailInput`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Validation du JSON entrant (`c.ShouldBindJSON`).
// @Description  2. Chargement de la `PreAuthSession` depuis Redis L1 (`cache_service.GetPreAuthSession`).
// @Description  3. Vérification que la méthode Email (`1`) est présente dans `preAuth.AvailableMethods` (ou `preAuth.Method == 1`).
// @Description  4. Résolution de l'adresse email et du pseudonyme du destinataire (depuis `SignUpData` si `flow == "signup"`, ou via `loadUserByIDCascade` si `flow == "login"`).
// @Description  5. Génération d'un nouveau code aléatoire à 6 chiffres et de son hash HMAC-SHA256 (`security.GenerateEmailOTP`).
// @Description  6. Mise à jour de `preAuth.CodeHash` dans Redis L1 (`cache_service.SavePreAuthSession`).
// @Description  7. Envoi asynchrone du job d'email (`template = "two_factor_code"`) dans la file Redis du Worker SMTP via `email_service.DispatchEmail`.
// @Description  8. Renvoi immédiat d'un statut HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Aucun (corps vide).
// @Description  - Persistence guarantees: Nouveau `CodeHash` sauvegardé en RAM L1 (`redis.PreAuth`) avec renouvellement du TTL de 5 minutes.
// @Description  - Side effects: Invalidation immédiate de tout code OTP Email précédemment émis pour ce `pre_auth_token` et mise en file d'attente d'un nouvel email SMTP.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Missing pre_auth_token:**
// @Description    - Trigger: Le champ `pre_auth_token` est vide ou le corps JSON est mal formé.
// @Description    - Required request/state: Requête JSON invalide.
// @Description    - Execution stage: Validation GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[TWO_FACTOR_SESSION_EXPIRED] PreAuth session expired or not found:**
// @Description    - Trigger: Le `pre_auth_token` fourni n'existe pas ou a expiré dans Redis L1.
// @Description    - Required request/state: Token inconnu ou périmé (> 5 min).
// @Description    - Execution stage: `cache_service.GetPreAuthSession`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorExpired` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Max attempts reached or Email method not allowed:**
// @Description    - Trigger: La session a atteint le quota maximal d'échecs (5) OU la méthode Email (`1`) n'est pas autorisée pour ce `pre_auth_token`.
// @Description    - Required request/state: Session révoquée ou méthode Email non activée.
// @Description    - Execution stage: `GetPreAuthSession` ou contrôle `AvailableMethods` dans `SendTwoFactorEmailCode`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] OTP generation or Redis L1 failure:**
// @Description    - Trigger: Échec du CSPRNG lors de `GenerateEmailOTP` ou échec d'écriture dans `SavePreAuthSession`.
// @Description    - Required request/state: Panne Redis L1 ou erreur système.
// @Description    - Execution stage: `auth_service.SendTwoFactorEmailCode`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        input body     two_factor_models.SendTwoFactorEmailInput true "PreAuthToken de la session en attente"
// @Success      200
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Expired PreAuth Session"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden or Email 2FA Not Enabled"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/verify-2fa/email/send [post]
func SendTwoFactorEmailCodeHandler(c *gin.Context) {
	var input two_factor_models.SendTwoFactorEmailInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	if err := two_factor_service.SendTwoFactorEmailCode(c.Request.Context(), input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.Status(http.StatusOK)
}
