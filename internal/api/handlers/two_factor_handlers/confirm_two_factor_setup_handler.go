package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// ConfirmTwoFactorSetupHandler godoc
// @Summary      Confirmer et activer une méthode de double authentification (2FA)
// @Description  Vérifie la preuve cryptographique ou le code à 6 chiffres associé au `setup_token` et active la méthode dans le bitmask 2FA de l'utilisateur tout en conservant ses autres méthodes déjà configurées.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization`) et une signature HMAC Ratchet valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: L'utilisateur appelant doit être le propriétaire légitime de la `PreAuthSession` d'enrôlement.
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `setup_token` (chaîne).
// @Description  - Optional fields: `code` (chaîne numérique à 6 chiffres pour Email ou TOTP), `passkey_registration` (objet `PasskeyRegistrationResponse` pour Passkey).
// @Description  - Validation rules: Binding GIN sur `two_factor_models.ConfirmTwoFactorSetupInput` + vérification métier de présence selon la méthode de la session.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant de l'appelant (`pkg.GetUserIDFromContext`) et validation du JSON (`c.ShouldBindJSON`).
// @Description  2. Récupération de la `PreAuthSession` en RAM L1 (`cache_service.GetPreAuthSession`) et vérification que `flow == "setup"` et `session.UserID == userID`.
// @Description  3. Chargement de l'utilisateur via la cascade L1 -> L2 -> L3 (`loadUserByIDCascade`).
// @Description  4. Validation de la preuve selon `session.Method` :
// @Description     - Email (`1`) : Comparaison en temps constant du HMAC-SHA256 du code. En cas d'échec, incrémentation du compteur (`IncrementPreAuthAttempts`).
// @Description     - TOTP (`2`) : Validation RFC 6238 (`security.VerifyTOTP`) et sauvegarde du secret dans `userPayload.TOTPSecret`.
// @Description     - Passkey (`3`) : Validation WebAuthn (`security.VerifyPasskeyRegistration`), écriture dans l'Object Cache L1 (`SetPasskeyInObjectCache`) et persistance asynchrone via `redis.EnqueueDB` (`EntityPasskey`, `ActionCreate`).
// @Description  5. Mise à jour du bitmask `userPayload.TwoFactorMethod |= variables.MethodToBit(session.Method)` et `TwoFactorEnabled = true`.
// @Description  6. Suppression de la session temporaire L1 (`DeletePreAuthSession`), mise à jour du cache utilisateur L1 (`SetUserInCache`) et persistance Write-Behind (`redis.EnqueueDB` sur `EntityUser`).
// @Description  7. Renvoi d'un statut HTTP 200 OK sans corps.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Aucun (corps vide).
// @Description  - Persistence guarantees: Mise à jour synchrone en RAM L1 et persistance asynchrone dans MongoDB (L2) et PostgreSQL (L3) via `redis.EnqueueDB`.
// @Description  - Side effects: Destruction immédiate du `setup_token` en L1, ajout éventuel d'une entrée dans `auth.user_passkeys`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Missing or malformed proof:**
// @Description    - Trigger: `setup_token` manquant, `code` non numérique, ou absence du code / `passkey_registration` requis pour la méthode en cours.
// @Description    - Required request/state: Payload JSON incomplet ou mal typé.
// @Description    - Execution stage: Validation GIN ou vérification des champs dans `ConfirmTwoFactorSetup`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[TWO_FACTOR_SESSION_EXPIRED] Setup session expired or not found:**
// @Description    - Trigger: Le `setup_token` n'existe plus dans Redis L1 (TTL de 5 minutes dépassé ou déjà consommé).
// @Description    - Required request/state: Token expiré ou inconnu.
// @Description    - Execution stage: `cache_service.GetPreAuthSession`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorExpired` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[TWO_FACTOR_INVALID_PROOF] Invalid OTP, TOTP or Passkey attestation:**
// @Description    - Trigger: Le code à 6 chiffres est erroné ou l'attestation WebAuthn ne correspond pas au challenge/RP ID.
// @Description    - Required request/state: Preuve cryptographique ou code incorrect (< 5 tentatives).
// @Description    - Execution stage: Vérification cryptographique dans `ConfirmTwoFactorSetup`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorInvalid` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Max attempts reached or session mismatch:**
// @Description    - Trigger: Le nombre d'essais atteint `variables.TwoFactorMaxAttempts` (5), ou le `setup_token` appartient à un autre utilisateur / un autre flux.
// @Description    - Required request/state: 5 échecs consécutifs ou usurpation de token.
// @Description    - Execution stage: `GetPreAuthSession`, `IncrementPreAuthAttempts` ou contrôle d'appartenance.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Write-Behind queue or database failure:**
// @Description    - Trigger: Échec critique de `redis.EnqueueDB` ou des lectures de fallback L3.
// @Description    - Required request/state: Panne Redis ou PostgreSQL.
// @Description    - Execution stage: Persistance Write-Behind.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                         true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                         true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                         true "Timestamp Unix de la requête"
// @Param        input         body   two_factor_models.ConfirmTwoFactorSetupInput   true "Token de configuration et preuve 2FA"
// @Success      200
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Expired Token or Invalid Proof"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Max Attempts Reached or Forbidden"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/2fa/confirm [post]
func ConfirmTwoFactorSetupHandler(c *gin.Context) {
	userID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input two_factor_models.ConfirmTwoFactorSetupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	if err := two_factor_service.ConfirmTwoFactorSetup(c.Request.Context(), userID, input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.Status(http.StatusOK)
}
