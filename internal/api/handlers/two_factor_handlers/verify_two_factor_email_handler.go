package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// VerifyTwoFactorEmailHandler godoc
// @Summary      Vérifier le second facteur par Email (OTP)
// @Description  Valide le code OTP à 6 chiffres envoyé par email lors d'une connexion (Login) ou d'une inscription (SignUp) et finalise l'ouverture de la session applicative sécurisée.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (Route publique protégée par `pre_auth_token` éphémère).
// @Description  - Required permissions or roles: Possession d'un `pre_auth_token` actif en RAM L1 autorisant la méthode Email (`1`).
// @Description  - Relevant middleware: MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `pre_auth_token` (chaîne), `code` (chaîne numérique stricte de 6 chiffres).
// @Description  - Optional fields: Aucun.
// @Description  - Validation rules: Binding GIN standard sur `two_factor_models.VerifyTwoFactorEmailInput` (`required,len=6,numeric`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Validation du JSON entrant (`c.ShouldBindJSON`).
// @Description  2. Chargement de la session de pré-authentification depuis Redis L1 (`cache_service.GetPreAuthSession`).
// @Description  3. Vérification que la méthode Email (`1`) fait partie des méthodes autorisées (`AvailableMethods`) pour cette session.
// @Description  4. Calcul du HMAC-SHA256 du code soumis (`security.HashEmailOTP`) et comparaison en temps constant avec `preAuth.CodeHash`.
// @Description     - En cas d'échec, incrémentation du compteur d'essais (`IncrementPreAuthAttempts`) et révocation immédiate si la limite de 5 tentatives est atteinte.
// @Description  5. Destruction immédiate de la `PreAuthSession` en RAM L1 (`DeletePreAuthSession`).
// @Description  6. Finalisation selon le flux (`preAuth.Flow`) :
// @Description     - Si `"signup"` : Création de l'utilisateur (`EmailVerified = true`), de sa session Ratchet et de ses UserSettings en L1 + persistance Write-Behind (`redis.EnqueueDB`) + mise à jour du Cuckoo Filter + envoi asynchrone de l'email de bienvenue.
// @Description     - Si `"login"` : Chargement de l'utilisateur (Cascade L1 -> L2 -> L3), récupération/création de la session d'appareil, dérivation du secret Ratchet, génération du MasterToken/JWT, réparation du Speed Cache et persistance Write-Behind (`redis.EnqueueDB`).
// @Description  7. Renvoi du `VerifyTwoFactorOutput` contenant les tokens finaux au client.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `two_factor_models.VerifyTwoFactorOutput` contenant `flow`, `user_id`, `master_token`, `jwt`, `expires_at`, `avatar` (si signup) et `message`.
// @Description  - Persistence guarantees: Persistance asynchrone de la session (et du compte si signup) dans MongoDB (L2) et PostgreSQL (L3) via `redis.EnqueueDB`.
// @Description  - Side effects: Destruction du `pre_auth_token` en L1, hydratation de la session L1 et du Speed Cache, mise à jour de l'historique IP.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON format or validation failed:**
// @Description    - Trigger: `pre_auth_token` manquant ou `code` ne respectant pas le format numérique à 6 chiffres.
// @Description    - Required request/state: Corps de requête invalide ou incomplet.
// @Description    - Execution stage: Validation GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse` avec détails d'erreur.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[TWO_FACTOR_SESSION_EXPIRED] PreAuth session expired or not found:**
// @Description    - Trigger: Le `pre_auth_token` est introuvable dans Redis L1 ou son TTL de 5 minutes a expiré.
// @Description    - Required request/state: Token expiré, déjà consommé ou invalide.
// @Description    - Execution stage: `cache_service.GetPreAuthSession`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorExpired` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[TWO_FACTOR_INVALID_PROOF] Incorrect Email OTP code:**
// @Description    - Trigger: Le hash du code soumis ne correspond pas au `CodeHash` stocké dans la `PreAuthSession`.
// @Description    - Required request/state: Code à 6 chiffres erroné (< 5 tentatives).
// @Description    - Execution stage: Vérification HMAC dans `auth_service.VerifyTwoFactorEmail`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorInvalid` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Max attempts reached or Email method not allowed:**
// @Description    - Trigger: L'utilisateur a atteint 5 tentatives échouées (session révoquée) OU la méthode Email n'est pas autorisée pour ce `pre_auth_token`.
// @Description    - Required request/state: Bruteforce détecté ou méthode non configurée sur le compte.
// @Description    - Execution stage: `GetPreAuthSession`, contrôle `AvailableMethods`, ou `IncrementPreAuthAttempts`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Database, Cache or Token generation failure:**
// @Description    - Trigger: Échec de lecture L3 PostgreSQL, de génération des tokens JWT/MasterToken ou d'écriture Redis.
// @Description    - Required request/state: Panne d'infrastructure interne.
// @Description    - Execution stage: Finalisation de session (`finalizeLoginSession` / `finalizeSignupAfterTwoFactor`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        input body     two_factor_models.VerifyTwoFactorEmailInput true "PreAuthToken et code OTP reçu par email"
// @Success      200  {object}  two_factor_models.VerifyTwoFactorOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Expired Session or Invalid Code"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden or Max Attempts Reached"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/verify-2fa/email [post]
func VerifyTwoFactorEmailHandler(c *gin.Context) {
	var input two_factor_models.VerifyTwoFactorEmailInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	output, err := two_factor_service.VerifyTwoFactorEmail(c.Request.Context(), input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
