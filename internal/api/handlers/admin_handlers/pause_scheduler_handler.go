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

// PauseSchedulerHandler godoc
// @Summary      Mettre en pause globale l'Activity-Aware Scheduler (Administrateur uniquement)
// @Description  Active une pause globale auto-expirante (TTL de sécurité par défaut 30 minutes, borné à 24h) dans Redis Core (`scheduler:global:paused`) et déclenche immédiatement la préemption coopérative (<200ms) avec sauvegarde de checkpoint sur tout job actif. Utile avant une migration de schéma, un redémarrage planifié ou une intervention d'urgence.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun (corps JSON `admin_models.PauseSchedulerInput` optionnel ; valeurs par défaut `ttl_minutes: 30`, `reason: "admin_pause"`).
// @Description  - Optional fields: `ttl_minutes` (entier `0..1440`), `reason` (chaîne max 256 caractères).
// @Description  - Validation rules: Aucun paramètre d'URL ou de query string n'est accepté ; bornes vérifiées dans `input.Validate()`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant administrateur (`callerID`) et du grade depuis le contexte Gin.
// @Description  2. Vérification stricte du grade Administrateur (rejet 403 sinon).
// @Description  3. Désérialisation optionnelle du corps JSON (`admin_models.PauseSchedulerInput`) en tolérant un corps vide (`io.EOF`).
// @Description  4. Délégation à `admin_service.PauseSchedulerService` (validation + écriture clé TTL dans Redis Core + préemption coopérative locale).
// @Description  5. Retour du DTO de confirmation `admin_models.SchedulerActionOutput` en HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `admin_models.SchedulerActionOutput` (`status: "paused"`, `ttl_minutes`, `reason`).
// @Description  - Persistence guarantees: Clé Redis Core avec TTL auto-expirant garantissant la reprise automatique même en cas d'oubli opérateur.
// @Description  - Side effects: Préemption coopérative immédiate de tout job lourd en cours et journalisation d'avertissement structurée.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON or out-of-range parameters:**
// @Description    - Trigger: Corps JSON malformé, `ttl_minutes` hors `[0..1440]` ou `reason` > 256 caractères.
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
// @Description  - **[CACHE_ERROR] Redis pause state write failure:**
// @Description    - Trigger: Indisponibilité de Redis Core lors de l'activation de la pause globale.
// @Description    - Execution stage: Écriture dans `cache_service.SetSchedulerGlobalPause`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                             true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                             true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                             true  "Timestamp Unix de la requête"
// @Param        input         body   admin_models.PauseSchedulerInput   false "Durée de pause (minutes) et motif"
// @Success      200  {object}  admin_models.SchedulerActionOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/scheduler/pause [post]
func PauseSchedulerHandler(c *gin.Context) {
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

	var input admin_models.PauseSchedulerInput
	if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	out, errSvc := admin_service.PauseSchedulerService(c.Request.Context(), callerID, grade, &input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
