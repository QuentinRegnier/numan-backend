package variables_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/variables_service"
	"github.com/gin-gonic/gin"
)

// GetVariablesHandler godoc
// @Summary      Récupérer les variables dynamiques de l'application
// @Description  Permet à un administrateur de consulter l'intégralité des variables de configuration dynamiques du backend, leurs valeurs actives en mémoire (incluant les surcharges stockées dans Redis), leurs valeurs par défaut compilées ainsi que leurs métadonnées de typage et de catégorie.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le `userID` extrait du contexte doit correspondre à un compte actif possédant strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun paramètre d'URL, aucune query string et aucun corps JSON requis.
// @Description  - Validation rules: Vérification de l'identité de l'appelant via `pkg.GetUserIDFromContext`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation du `callerID` depuis le contexte Gin sécurisé.
// @Description  2. Contrôle d'accès Zero-Trust (`security_service.LeftAdmin`) en cascade : vérification du grade en RAM L1 (`SpeedCache`) -> Fallback L2 (MongoDB) -> Fallback L3 (PostgreSQL) avec auto-guérison L3->L2.
// @Description  3. Vérification de fraîcheur en $O(1)$ (`EnsureVariablesFreshness`) : compare le numéro de version local avec la clé `system:variables:version` dans Redis.
// @Description  4. Si la version distante Redis a évolué, rechargement atomique du Hash `system:variables:overrides` et de la version via script Lua (`LoadVariablesStateAtomic`) et application en mémoire. En cas d'indisponibilité temporaire de Redis, repli transparent sur le snapshot mémoire local.
// @Description  5. Capture atomique d'un snapshot thread-safe (`sync.RWMutex`) de toutes les variables enregistrées (`variables.GetSnapshot`).
// @Description  6. Construction et renvoi du DTO `variables_models.GetVariablesOutput` contenant la version active, la table clé-valeur rapide (`variables`) et les métadonnées complètes (`details`).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `variables_models.GetVariablesOutput`.
// @Description  - Persistence guarantees: Lecture pure sans mutation de l'état de référence. Si une réconciliation Redis a eu lieu lors de l'étape 4, la RAM locale de l'instance est mise à jour.
// @Description  - Side effects: Réhydratation asynchrone L3->L2 du profil administrateur en cas de Cache Miss complet sur `LeftAdmin`, et mise à niveau éventuelle du cache RAM local des variables si un retard de version a été détecté.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_USER_ID] Malformed user ID in context:**
// @Description    - Trigger: L'identifiant utilisateur stocké sous forme de chaîne dans le contexte ne peut pas être converti en `int64`.
// @Description    - Execution stage: Extraction de l'identité (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `"INVALID_USER_ID"` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Invalid or zero caller ID:**
// @Description    - Trigger: Le JWT est manquant/expiré (intercepté par `JWTMiddleware`) ou le `callerID` extrait est inférieur ou égal à `0`.
// @Description    - Execution stage: Middleware d'authentification / Contrôle initial `LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing user in context:**
// @Description    - Trigger: La clé `userID` est absente du contexte Gin.
// @Description    - Execution stage: Extraction de l'identité (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `"UNAUTHORIZED"` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Insufficient privileges or suspended account:**
// @Description    - Trigger: L'utilisateur possède un grade inférieur à `UserGradeAdmin` (`4`), ou son compte est banni/désactivé.
// @Description    - Execution stage: Contrôle d'autorisation métier (`security_service.LeftAdmin`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Administrator account does not exist:**
// @Description    - Trigger: Le `callerID` est introuvable dans les trois couches de données (L1 SpeedCache, L2 MongoDB, L3 PostgreSQL).
// @Description    - Execution stage: Recherche en cascade dans `security_service.LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Context type error or L3 PostgreSQL failure:**
// @Description    - Trigger: Le `userID` dans le contexte a un type Go inattendu, ou la requête SQL de fallback L3 échoue lors de la vérification du compte administrateur.
// @Description    - Execution stage: Extraction du contexte / Fallback L3 dans `security_service.LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         admin
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Success      200  {object}  variables_models.GetVariablesOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid User ID"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin grade required)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Admin User Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /variables/get [get]
func GetVariablesHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	output, err := variables_service.GetVariables(c.Request.Context(), callerID)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
