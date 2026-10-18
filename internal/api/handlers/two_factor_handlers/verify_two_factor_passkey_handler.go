package two_factor_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/two_factor_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/two_factor_service"
	"github.com/gin-gonic/gin"
)

// VerifyTwoFactorPasskeyHandler godoc
// @Summary      Vérifier le second facteur par Passkey (FIDO2 / WebAuthn)
// @Description  Valide une assertion cryptographique FIDO2/WebAuthn (`webauthn.get` lors d'un Login) ou un enregistrement de clé matérielle (`webauthn.create` lors d'un SignUp) en vérifiant le challenge, l'origine RP ID, les flags matériels UP/UV, la signature ECDSA P-256 / Ed25519 et la monotonie du compteur `SignCount`.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (Route publique protégée par `pre_auth_token` et challenge WebAuthn).
// @Description  - Required permissions or roles: Possession d'un `pre_auth_token` actif en RAM L1 autorisant la méthode Passkey (`3`).
// @Description  - Relevant middleware: MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `pre_auth_token` (chaîne).
// @Description  - Conditional fields: `passkey_assertion` (requis si `flow == "login"`) OU `passkey_registration` (requis si `flow == "signup"`).
// @Description  - Validation rules: Binding GIN sur `two_factor_models.VerifyTwoFactorPasskeyInput` et vérification structurelle selon le flux de la session.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Validation du JSON entrant (`c.ShouldBindJSON`).
// @Description  2. Chargement de la `PreAuthSession` depuis Redis L1 (`cache_service.GetPreAuthSession`) et contrôle d'autorisation de la méthode Passkey (`3`).
// @Description  3. **Cas Inscription (`flow == "signup"`) :**
// @Description     - Vérification de l'enregistrement WebAuthn (`security.VerifyPasskeyRegistration`) : validation du challenge, du RP ID, des flags UP/UV et normalisation de la clé publique P-256 ou Ed25519.
// @Description     - Construction du `PasskeyPayload`, suppression de la `PreAuthSession` L1, et appel de `finalizeSignupAfterTwoFactor` (création User, Session, Settings et Passkey en L1 + Write-Behind `redis.EnqueueDB`).
// @Description  4. **Cas Connexion (`flow == "login"`) :**
// @Description     - Chargement de la Passkey par `CredentialID` via la cascade L1 -> L2 -> L3 (`object_cache_service.GetPasskeyByCredentialIDCascade`) et vérification qu'elle appartient à `preAuth.UserID`.
// @Description     - Vérification de la signature cryptographique et du compteur matériel (`security.VerifyPasskeyAssertion`).
// @Description     - Si un clonage matériel est détecté (`incomingCount <= storedSignCount`), destruction immédiate de la `PreAuthSession` et rejet avec `CodePasskeyCloneAlert`.
// @Description     - Mise à jour du `SignCount` en L1 (`SetPasskeyInObjectCache`) et en Write-Behind (`redis.EnqueueDB` sur `EntityPasskey`), puis ouverture de la session via `finalizeLoginSession`.
// @Description  5. Renvoi du `VerifyTwoFactorOutput` au client.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `two_factor_models.VerifyTwoFactorOutput` contenant `flow`, `user_id`, `master_token`, `jwt`, `expires_at`, `avatar` (si signup) et `message`.
// @Description  - Persistence guarantees: Mise à jour asynchrone du `sign_count` de la Passkey (ou création de la Passkey au signup) et de la session Ratchet vers MongoDB (L2) et PostgreSQL (L3) via `redis.EnqueueDB`.
// @Description  - Side effects: Destruction du `pre_auth_token` en L1, rafraîchissement de l'Object Cache Passkey L1 et du Speed Cache utilisateur.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Missing assertion/registration payload:**
// @Description    - Trigger: `pre_auth_token` manquant, ou absence de `passkey_assertion` lors d'un login / absence de `passkey_registration` lors d'un signup.
// @Description    - Required request/state: Corps JSON incomplet pour le flux concerné.
// @Description    - Execution stage: Validation GIN ou vérification de structure dans `VerifyTwoFactorPasskey`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[TWO_FACTOR_SESSION_EXPIRED] PreAuth session expired or not found:**
// @Description    - Trigger: Le `pre_auth_token` n'existe plus dans Redis L1.
// @Description    - Required request/state: Session de pré-authentification expirée (> 5 min) ou déjà utilisée.
// @Description    - Execution stage: `cache_service.GetPreAuthSession`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorExpired` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[TWO_FACTOR_INVALID_PROOF] Invalid WebAuthn signature, challenge, RP ID or unknown credential:**
// @Description    - Trigger: La Passkey n'appartient pas à l'utilisateur, les flags biométriques UP/UV sont absents, le challenge ne correspond pas, ou la signature P-256/Ed25519 est invalide.
// @Description    - Required request/state: Preuve WebAuthn invalide (< 5 tentatives).
// @Description    - Execution stage: `GetPasskeyByCredentialIDCascade` / `VerifyPasskeyAssertion` / `VerifyPasskeyRegistration`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTwoFactorInvalid` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[PASSKEY_CLONE_DETECTED] Authenticator sign counter regression:**
// @Description    - Trigger: Le compteur matériel `SignCount` renvoyé par l'authentificateur est inférieur ou égal au `SignCount` précédemment enregistré.
// @Description    - Required request/state: Suspicion de clonage de clé matérielle FIDO2.
// @Description    - Execution stage: `security.VerifyPasskeyAssertion`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodePasskeyCloneAlert` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Max attempts reached or Passkey method not enabled:**
// @Description    - Trigger: 5 tentatives échouées consécutives OU la méthode Passkey n'est pas autorisée pour cette session.
// @Description    - Required request/state: Dépassement du seuil anti-bruteforce ou méthode non disponible.
// @Description    - Execution stage: `GetPreAuthSession`, contrôle `AvailableMethods`, ou `IncrementPreAuthAttempts`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Database or Session persistence failure:**
// @Description    - Trigger: Échec de lecture L3 ou d'écriture de la session finale.
// @Description    - Required request/state: Panne d'infrastructure Redis ou PostgreSQL.
// @Description    - Execution stage: Finalisation de session.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        input body     two_factor_models.VerifyTwoFactorPasskeyInput true "PreAuthToken et assertion/enregistrement Passkey"
// @Success      200  {object}  two_factor_models.VerifyTwoFactorOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Expired Session or Invalid WebAuthn Proof"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Passkey Clone Alert or Max Attempts Reached"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /auth/verify-2fa/passkey [post]
func VerifyTwoFactorPasskeyHandler(c *gin.Context) {
	var input two_factor_models.VerifyTwoFactorPasskeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de requête invalide.", err))
		return
	}

	output, err := two_factor_service.VerifyTwoFactorPasskey(c.Request.Context(), input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
