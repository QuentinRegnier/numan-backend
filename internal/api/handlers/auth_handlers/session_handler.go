package auth_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/auth_service"
	"github.com/gin-gonic/gin"
)

// GetSessionsHandler godoc
// @Summary      Lister les sessions actives
// @Description  Récupère la liste exhaustive des sessions (appareils) actuellement connectées pour l'utilisateur courant.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC.
// @Description  - Required permissions or roles: Aucune.
// @Description  - Relevant middleware: JWTMiddleware (authentification), HMACMiddleware (intégrité), MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun corps requis. L'identifiant `userID` est extrait automatiquement depuis le contexte JWT.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction du `userID` sécurisé depuis le contexte (`pkg.GetUserIDFromContext`).
// @Description  2. Interrogation directe de la source de vérité (PostgreSQL L3) via la fonction `FuncLoadUserSessionsView` pour obtenir l'historique complet et à jour.
// @Description  3. Mapping des entités brutes vers des objets `auth_models.SessionView`, expurgeant scrupuleusement toute donnée sensible (MasterToken, secrets cryptographiques).
// @Description  4. Construction du tableau de réponse, en garantissant un tableau vide `[]` au lieu de `null` en cas d'absence de session.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Un tableau JSON `[]auth_models.SessionView` listant les identifiants de session, les données d'appareil (`device_info`), l'historique des IPs, et les dates de création/expiration.
// @Description  - Persistence guarantees: Lecture seule (Aucune modification d'état).
// @Description  - Side effects: Aucun.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Database lookup failure:**
// @Description    - Trigger: Échec de la requête vers PostgreSQL (L3).
// @Description    - Required request/state: Base de données injoignable ou erreur SQL.
// @Description    - Execution stage: Chargement en base (`postgres.FuncLoadUserSessionsView`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         sessions
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Success      200  {array}   auth_models.SessionView
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /session/get [get]
func GetSessionsHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	sessions, err := auth_service.GetUserSessions(c.Request.Context(), callerID)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, sessions)
}
