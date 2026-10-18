package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// SetupTwoFactorHandler godoc
// @Summary      Initier l'activation d'une méthode de double authentification (2FA)
// @Description  Génère une session d'enrôlement 2FA temporaire en RAM (L1) et délivre le challenge adapté (envoi d'un OTP par email, génération d'un secret TOTP Base32 + URI QR Code, ou challenge WebAuthn FIDO2) sans écraser les autres méthodes 2FA déjà configurées sur le compte.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization`) et une signature HMAC Ratchet valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: L'utilisateur appelant doit être authentifié et actif.
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `method` (entier `1` pour Email, `2` pour TOTP, `3` pour Passkey).
// @Description  - Optional fields: `device_name` (chaîne, max 64 caractères, nom de l'appareil pour une Passkey).
// @Description  - Validation rules: Binding GIN standard sur `two_factor_models.SetupTwoFactorInput`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant de l'utilisateur appelant via `pkg.GetUserIDFromContext`.
// @Description  2. Validation du JSON entrant (`c.ShouldBindJSON`).
// @Description  3. Chargement du profil utilisateur via la cascade L1 (Redis) -> L2 (MongoDB) -> L3 (PostgreSQL) avec auto-guérison.
// @Description  4. Génération d'un `setup_token` temporaire et initialisation d'une `PreAuthSession` (`flow = "setup"`, TTL 5 minutes).
// @Description  5. Selon la méthode choisie :
// @Description     - `1` (Email) : Génération d'un code OTP à 6 chiffres haché en HMAC-SHA256, sauvegarde en L1 et envoi asynchrone via `email_service.DispatchEmail`.
// @Description     - `2` (TOTP) : Génération d'un secret aléatoire de 160 bits en Base32 et de l'URI `otpauth://` conforme RFC 6238, sauvegarde en L1.
// @Description     - `3` (Passkey) : Génération d'un challenge cryptographique WebAuthn de 32 octets en Base64URL, sauvegarde en L1.
// @Description  6. Renvoi immédiat du `SetupTwoFactorOutput` au client.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `two_factor_models.SetupTwoFactorOutput` contenant le `setup_token`, la méthode et les paramètres d'enrôlement (`totp_secret`, `totp_qr_uri` ou `passkey_options`).
// @Description  - Persistence guarantees: État d'enrôlement conservé en RAM L1 (`redis.PreAuth`) pendant 5 minutes. Aucune mutation en base L2/L3 tant que `ConfirmTwoFactorSetup` n'a pas été validé.
// @Description  - Side effects: Envoi asynchrone d'un email OTP via la file Redis du Worker SMTP si `method == 1`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON format or unsupported method:**
// @Description    - Trigger: Le champ `method` est absent, hors de `[1, 2, 3]`, ou `device_name` dépasse 64 caractères.
// @Description    - Required request/state: Corps JSON mal formé ou hors contrat.
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
// @Description  - **[RESOURCE_NOT_FOUND] User not found:**
// @Description    - Trigger: L'utilisateur extrait du JWT n'existe dans aucune couche de stockage (L1, L2, L3).
// @Description    - Required request/state: Compte supprimé ou inexistant.
// @Description    - Execution stage: `loadUserByIDCascade`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Cryptographic RNG or Redis L1 failure:**
// @Description    - Trigger: Échec de génération du secret/challenge ou indisponibilité de Redis L1 (`SavePreAuthSession`).
// @Description    - Required request/state: Panne d'infrastructure Redis ou CSPRNG.
// @Description    - Execution stage: `auth_service.SetupTwoFactor`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   two_factor_models.SetupTwoFactorInput   true "Paramètres d'initialisation 2FA"
// @Success      200  {object}  two_factor_models.SetupTwoFactorOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden / Unauthorized"
// @Failure      404  {object}  numan_error.PublicErrorResponse "User Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/2fa/setup [post]
func SetupTwoFactorHandler(c *gin.Context) {
	userID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input two_factor_models.SetupTwoFactorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	output, err := two_factor_service.SetupTwoFactor(c.Request.Context(), userID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
