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

// GetTelemetryOverviewHandler godoc
// @Summary      Consulter la télémétrie datacenter temps réel (Administrateur uniquement)
// @Description  Retourne le snapshot unifié de télémétrie d'un datacenter (saturation globale, score d'activité, régime du Scheduler, alertes hors-pic et anti-famine 48h, topologie des 3 tiers Redis, pools PostgreSQL/MongoDB, stress par serveur physique avec `bottleneck_hint`, agrégats GeoIP ISO-2 privacy-safe sur 24h et historique glissant 1-minute). Lu en moins de 1ms depuis Redis Core (`infra:telemetry:dc:<dc_id>:live`) avec agrégation de secours transparente si le cache est encore froid.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun (le corps JSON `admin_models.GetTelemetryOverviewInput` est optionnel ; s'il est vide, le datacenter courant `DATACENTER_ID` est sélectionné par défaut).
// @Description  - Optional fields: `datacenter` (chaîne alphanumérique `[a-zA-Z0-9._-]`, 1 à 64 caractères).
// @Description  - Validation rules: Aucun paramètre d'URL ou de query string n'est accepté ; si un corps JSON est fourni, il doit être valide et respecter le format `validDatacenterIDPattern`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant administrateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Contrôle d'accès strict : vérifie `user_grade == UserGradeAdmin` (4). Rejet 403 immédiat sinon.
// @Description  3. Désérialisation optionnelle du corps JSON (`admin_models.GetTelemetryOverviewInput`) en tolérant un corps vide (`io.EOF`).
// @Description  4. Délégation à `admin_service.GetTelemetryOverviewService` (validation métier + lecture L1 Redis Core / agrégation de secours via `infra_telemetry_service`).
// @Description  5. Retour du snapshot typé `admin_models.GetTelemetryOverviewOutput` en HTTP 200 OK.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `admin_models.GetTelemetryOverviewOutput`.
// @Description  - Persistence guarantees: Lecture seule en L1 Redis Core (ou rafraîchissement du snapshot live en cas de démarrage à froid).
// @Description  - Side effects: Journalisation structurée de l'accès administratif (`admin_id`, `datacenter`, `regime`).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON or invalid datacenter identifier:**
// @Description    - Trigger: Corps JSON malformé ou champ `datacenter` contenant des caractères interdits ou > 64 caractères.
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
// @Description  - **[INTERNAL_SERVER_ERROR] Telemetry aggregation failure:**
// @Description    - Trigger: Erreur interne inattendue lors de la lecture ou de l'agrégation de la télémétrie.
// @Description    - Execution stage: Exécution de `admin_service.GetTelemetryOverviewService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                    true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                    true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                    true  "Timestamp Unix de la requête"
// @Param        input         body   admin_models.GetTelemetryOverviewInput    false "Filtre optionnel de datacenter"
// @Success      200  {object}  admin_models.GetTelemetryOverviewOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/telemetry/overview [post]
func GetTelemetryOverviewHandler(c *gin.Context) {
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

	var input admin_models.GetTelemetryOverviewInput
	if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	out, errSvc := admin_service.GetTelemetryOverviewService(c.Request.Context(), callerID, grade, &input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
