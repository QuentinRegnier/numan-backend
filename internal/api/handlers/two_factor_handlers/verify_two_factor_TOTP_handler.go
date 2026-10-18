package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// VerifyTwoFactorTOTPHandler godoc
// @Summary      Vérifier le second facteur par application Authenticator (TOTP)
// @Description  Valide un code TOTP à 6 chiffres conforme à la RFC 6238 (avec tolérance de dérive d'horloge ±1 pas de 30s et bouclier anti-rejeu atomique en RAM L1) lors d'un Login ou d'un SignUp, puis ouvre la session applicative.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (Route publique protégée par `pre_auth_token` éphémère).
// @Description  - Required permissions or roles: Possession d'un `pre_auth_token` actif en RAM L1 autorisant la méthode TOTP (`2`).
// @Description  - Relevant middleware: MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `pre_auth_token` (chaîne), `code` (chaîne numérique stricte de 6 chiffres générée par l'application Authenticator).
// @Description  - Optional fields: Aucun.
// @Description  - Validation rules: Binding GIN standard sur `two_factor_models.VerifyTwoFactorTOTPInput` (`required,len=6,numeric`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Validation du JSON entrant (`c.ShouldBindJSON`).
// @Description  2. Récupération de la `PreAuthSession` depuis Redis L1 (`cache_service.GetPreAuthSession`).
// @Description  3. Vérification que la méthode TOTP (`2`) est autorisée dans `preAuth.AvailableMethods`.
// @Description  4. Vérification mathématique du code selon la RFC 6238 (`security.VerifyTOTP`) avec le secret Base32 `preAuth.TempTOTPSecret`.
// @Description     - En cas d'échec, incrémentation du compteur (`IncrementPreAuthAttempts`) et révocation de la session au 5ème essai.
// @Description  5. Vérification anti-rejeu atomique via `cache_service.ConsumeTOTPCode` (`SetNX` sur une fenêtre de 90 secondes) pour interdire toute réutilisation d'un même code TOTP.
// @Description  6. Destruction immédiate de la `PreAuthSession` en RAM L1 (`DeletePreAuthSession`).
// @Description  7. Finalisation de l'inscription (`finalizeSignupAfterTwoFactor`) ou de la connexion (`finalizeLoginSession`) avec hydratation L1 et Write-Behind (`redis.EnqueueDB`).
// @Description  8. Renvoi du `VerifyTwoFactorOutput` contenant le MasterToken et le JWT.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `two_factor_models.VerifyTwoFactorOutput` contenant `flow`, `user_id`, `master_token`, `jwt`, `expires_at`, `avatar` (si signup) et `message`.
// @Description  - Persistence guarantees: Persistance asynchrone de la session (et de l'utilisateur avec son `totp_secret` si signup) vers MongoDB (L2) et PostgreSQL (L3) via `redis.EnqueueDB`.
// @Description  - Side effects: Verrouillage du code TOTP utilisé pendant 90s dans Redis L1, suppression du `pre_auth_token`, hydratation du Speed Cache et de la session L1.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON format or validation failed:**
// @Description    - Trigger: `pre_auth_token` absent ou `code` ne contenant pas exactement 6 chiffres numériques.
// @Description    - Required request/state: Payload JSON mal formé.
// @Description    - Execution stage: Validation GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[TWO_FACTOR_SESSION_EXPIRED] PreAuth session expired or not found:**
// @Description    - Trigger: Le `pre_auth_token` a expiré (TTL > 5 min) ou n'existe pas dans Redis L1.
// @Description    - Required request/state: Token inconnu ou périmé.
// @Description    - Execution stage: `cache_service.GetPreAuthSession`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorExpired` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[TWO_FACTOR_INVALID_PROOF] Incorrect TOTP code:**
// @Description    - Trigger: Le code à 6 chiffres ne correspond à aucune des fenêtres temporelles `[-1, 0, +1]` du secret TOTP.
// @Description    - Required request/state: Code TOTP erroné ou désynchronisé (< 5 tentatives).
// @Description    - Execution stage: `security.VerifyTOTP`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorInvalid` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Max attempts reached or TOTP method not enabled:**
// @Description    - Trigger: 5 tentatives échouées consécutives OU la méthode TOTP n'est pas configurée pour cette session.
// @Description    - Required request/state: Bruteforce détecté ou méthode TOTP inactive sur le compte.
// @Description    - Execution stage: `GetPreAuthSession`, contrôle `AvailableMethods`, ou `IncrementPreAuthAttempts`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[TWO_FACTOR_INVALID_PROOF] Replayed TOTP code:**
// @Description    - Trigger: Un code TOTP pourtant valide mathématiquement est soumis une seconde fois dans sa fenêtre de 90 secondes.
// @Description    - Required request/state: Rejeu d'un code déjà consommé (`ConsumeTOTPCode` renvoie `false`).
// @Description    - Execution stage: `cache_service.ConsumeTOTPCode`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorInvalid` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis L1, PostgreSQL L3 or Token generation failure:**
// @Description    - Trigger: Erreur interne lors du `SetNX` Redis, du chargement utilisateur L3 ou de la génération des tokens de session.
// @Description    - Required request/state: Services d'infrastructure inaccessibles.
// @Description    - Execution stage: `ConsumeTOTPCode` / `finalizeLoginSession` / `finalizeSignupAfterTwoFactor`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        input body     two_factor_models.VerifyTwoFactorTOTPInput true "PreAuthToken et code TOTP à 6 chiffres"
// @Success      200  {object}  two_factor_models.VerifyTwoFactorOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Expired Session or Invalid TOTP Code"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden, Max Attempts or Replay Detected"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/verify-2fa/totp [post]
func VerifyTwoFactorTOTPHandler(c *gin.Context) {
	var input two_factor_models.VerifyTwoFactorTOTPInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	output, err := two_factor_service.VerifyTwoFactorTOTP(c.Request.Context(), input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
