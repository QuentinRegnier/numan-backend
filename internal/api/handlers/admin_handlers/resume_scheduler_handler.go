package admin_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// ResumeSchedulerHandler godoc
// @Summary      Reprendre l'exécution de l'Activity-Aware Scheduler (Administrateur uniquement)
// @Description  Supprime immédiatement la clé de pause globale dans Redis Core (`scheduler:global:paused`) et réactive l'orchestration automatique des jobs selon le régime d'activité courant du datacenter. Les jobs précédemment préemptés reprendront exactement à leur dernier checkpoint sauvegardé lors de la prochaine fenêtre éligible.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun corps JSON ni paramètre d'URL requis.
// @Description  - Validation rules: Aucune.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant administrateur (`callerID`) et du grade depuis le contexte Gin.
// @Description  2. Vérification stricte du grade Administrateur (rejet 403 sinon).
// @Description  3. Délégation à `admin_service.ResumeSchedulerService` qui lève la pause dans Redis Core et sur l'instance locale si présente.
// @Description  4. Retour du DTO de confirmation `admin_models.SchedulerActionOutput` (`status: "resumed"`) en HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `admin_models.SchedulerActionOutput` (`status: "resumed"`).
// @Description  - Persistence guarantees: Suppression atomique de la clé `scheduler:global:paused` dans Redis Core.
// @Description  - Side effects: Le pod `scheduler_node` reprend son évaluation dès le tick suivant (<= 5 secondes) et journalisation structurée.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing or expired JWT:**
// @Description    - Trigger: Jeton d'authentification absent, expiré ou corrompu.
// @Description    - Execution stage: Extraction du contexte (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Administrator grade required:**
// @Description    - Trigger: L'utilisateur ne possède pas le grade Administrateur (4).
// @Description    - Execution stage: Contrôle des privilèges administratifs.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[CACHE_ERROR] Redis pause clear failure:**
// @Description    - Trigger: Indisponibilité de Redis Core lors de la suppression de la clé de pause.
// @Description    - Execution stage: Suppression dans `cache_service.ClearSchedulerGlobalPause`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Success      200  {object}  admin_models.SchedulerActionOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (Forbidden Query Parameters)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/scheduler/resume [post]
func ResumeSchedulerHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")
	if grade != variables.UserGradeAdmin && grade != variables.MemberRoleAdmin {
		numan_error.RespondWithError(c, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil))
		return
	}
	if c.Request != nil && c.Request.URL != nil && c.Request.URL.RawQuery != "" {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres d'URL (query string) sont interdits sur cette route.", nil))
		return
	}

	out, errSvc := admin_service.ResumeSchedulerService(c.Request.Context(), callerID, grade)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
