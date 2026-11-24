package admin_handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// GetSchedulerJobsHandler godoc
// @Summary      Lister l'état des jobs de l'Activity-Aware Scheduler (Administrateur uniquement)
// @Description  Retourne la vue consolidée de l'Activity-Aware Job Scheduler pour le datacenter cible : régime actif (`OVERLOAD`..`DEEP_IDLE`), état de pause globale, job actuellement en cours d'exécution, budget CPU alloué, alerte anti-famine 48h et liste complète des 24 jobs planifiés avec leurs checkpoints, statuts, compteurs de préemption et horodatages. En topologie distribuée (`api_node`), les états sont lus depuis Redis Core (`scheduler:job:*`) alimentés par le pod `scheduler_node`.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun (le corps JSON `admin_models.GetSchedulerJobsInput` est optionnel).
// @Description  - Optional fields: `datacenter` (chaîne `[a-zA-Z0-9._-]`, 1 à 64 caractères ; défaut : `DATACENTER_ID`).
// @Description  - Validation rules: Aucun paramètre d'URL ou de query string n'est accepté ; tout corps JSON fourni est validé via `input.Validate()`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant administrateur (`callerID`) et du grade depuis le contexte Gin.
// @Description  2. Vérification stricte du grade Administrateur (rejet 403 sinon).
// @Description  3. Désérialisation optionnelle du corps JSON (`admin_models.GetSchedulerJobsInput`).
// @Description  4. Délégation à `admin_service.GetSchedulerJobsService` qui interroge `scheduler_service.GetJobsOverview`.
// @Description  5. Retour du DTO `admin_models.GetSchedulerJobsOutput` en HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `admin_models.GetSchedulerJobsOutput`.
// @Description  - Persistence guarantees: Lecture seule en mémoire locale (`scheduler_node`/`monolith`) ou Redis Core (`api_node`).
// @Description  - Side effects: Journalisation structurée de la consultation administrative.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON or invalid datacenter identifier:**
// @Description    - Trigger: Corps JSON malformé ou `datacenter` invalide.
// @Description    - Execution stage: Désérialisation JSON (`c.ShouldBindJSON`) ou validation métier (`input.Validate()`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
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
// @Description  - **[INTERNAL_SERVER_ERROR] Unexpected scheduler overview failure:**
// @Description    - Trigger: Erreur interne imprévue lors de l'agrégation de l'état des jobs.
// @Description    - Execution stage: Exécution de `admin_service.GetSchedulerJobsService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                              true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                              true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                              true  "Timestamp Unix de la requête"
// @Param        input         body   admin_models.GetSchedulerJobsInput  false "Filtre optionnel de datacenter"
// @Success      200  {object}  admin_models.GetSchedulerJobsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/scheduler/jobs [post]
func GetSchedulerJobsHandler(c *gin.Context) {
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

	var input admin_models.GetSchedulerJobsInput
	if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	out, errSvc := admin_service.GetSchedulerJobsService(c.Request.Context(), callerID, grade, &input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
