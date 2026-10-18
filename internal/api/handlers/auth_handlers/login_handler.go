package auth_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/auth_service"
	"github.com/gin-gonic/gin"
)

// LoginHandler godoc
// @Summary      Connecter un utilisateur
// @Description  Authentifie un utilisateur via email/password et établit une session applicative sécurisée.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (Route publique).
// @Description  - Required permissions or roles: Aucune.
// @Description  - Relevant middleware: MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `email` (format email valide), `password_hash` (chaîne), `firebase_installation_id` (chaîne pour identifier l'appareil).
// @Description  - Optional fields: `device_info` (objet structuré contenant les infos de l'appareil client).
// @Description  - Validation rules: Binding GIN standard sur `auth_models.LoginInput`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Validation du JSON entrant (`c.ShouldBindJSON`).
// @Description  2. Chargement de l'utilisateur (Cascade MongoDB Warm Storage -> PostgreSQL Cold Storage).
// @Description     - Si l'utilisateur n'est présent que dans PostgreSQL (L3), déclenchement d'une auto-guérison (Write-Behind vers MongoDB L2).
// @Description  3. Contrôle des identifiants (comparaison des hash) et du statut du compte (Banni / Désactivé).
// @Description  4. Gestion de la session de l'appareil (Recherche en cascade Cache -> MongoDB -> PostgreSQL).
// @Description  5. Si l'appareil est inconnu ou session inexistante, création d'une nouvelle session. L'IP du client est requise.
// @Description  6. Génération des tokens sécurisés (MasterToken et JWT).
// @Description  7. Synchronisation et réparation des caches (Timeline, Speed Cache).
// @Description  8. Mise en file d'attente (Write-Behind Redis) de la session créée ou mise à jour pour persistance DB.
// @Description  9. Renvoi du UserID et des Tokens au client.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `auth_models.LoginResponse` contenant le UserID, MasterToken, JWT, l'expiration et un message. Note: Le profil complet n'est plus renvoyé ici (allégé).
// @Description  - Persistence guarantees: Mise à jour/création asynchrone des sessions en DB via la file d'attente Redis (Write-Behind).
// @Description  - Side effects: Hydratation de la timeline, Speed Cache synchronisé, historique IP de session mis à jour, auto-guérison de l'utilisateur (L3 vers L2).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON format or validation failed:**
// @Description    - Trigger: Le corps JSON manque d'un champ obligatoire ou contient un format invalide.
// @Description    - Required request/state: Corps de requête invalide ou manquant.
// @Description    - Execution stage: Validation GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse` avec détails d'erreur.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Incorrect email or password:**
// @Description    - Trigger: L'utilisateur n'est trouvé ni dans MongoDB ni dans PostgreSQL, OU le hash de mot de passe fourni ne correspond pas à la base.
// @Description    - Required request/state: Email inconnu, ou mot de passe incorrect.
// @Description    - Execution stage: Chargement de l'utilisateur ou vérification du mot de passe.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN] Account deactivated:**
// @Description    - Trigger: L'utilisateur a été trouvé et authentifié, mais son flag `Desactivated` est vrai.
// @Description    - Required request/state: Le compte est désactivé volontairement.
// @Description    - Execution stage: Contrôle de statut du compte.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[FORBIDDEN] Account banned:**
// @Description    - Trigger: L'utilisateur a été authentifié, mais son flag `Banned` est vrai.
// @Description    - Required request/state: Le compte est banni.
// @Description    - Execution stage: Contrôle de statut du compte.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Database or Token Generation failure:**
// @Description    - Trigger: Échec de connexion à PostgreSQL (L3) ou de génération de session.
// @Description    - Required request/state: Services internes inaccessibles.
// @Description    - Execution stage: Chargement base de données / Génération.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        input         body   auth_models.LoginInput true "Données de connexion de l'utilisateur"
// @Success      200  {object}  auth_models.LoginResponse
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Incorrect credentials"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Account banned or deactivated"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /login [post]
func LoginHandler(c *gin.Context) {
	var input auth_models.LoginInput

	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou contraintes non respectées.", err))
		return
	}

	response, err := auth_service.Login(c.Request.Context(), input, []string{c.ClientIP()})
	if err != nil {
		// Le service a déjà qualifié l'erreur (Banni, Identifiants incorrects, Désactivé)
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}
