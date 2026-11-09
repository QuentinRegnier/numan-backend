package admin_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/gin-gonic/gin"
)

// GetDelegationSwitchHandler godoc
// @Summary      Consulter l'état de l'interrupteur de délégation administrative
// @Description  Permet à un modérateur ou administrateur de consulter l'état d'activation de la délégation administrative. Lorsque l'interrupteur est actif (`true`), les contradictions entre pairs modérateurs sont automatiquement routées vers les Super-Modérateurs ($T_{\text{mod}} \ge 0.90$) en file `final_vote`. Lorsqu'il est inactif (`false`), toute contradiction remonte directement vers la file des Administrateurs (`admin`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun paramètre d'URL, query string ou corps JSON requis.
// @Description  - Validation rules: Vérification de l'identité de l'appelant via le contexte d'authentification.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Contrôle de l'authentification et des privilèges via les middlewares Gin.
// @Description  2. Interrogation atomique du drapeau booléen en mémoire vive (`moderation_service.GetDelegationSwitchService`).
// @Description  3. Construction et renvoi du DTO `moderation_models.AdminDelegationSwitchOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.AdminDelegationSwitchOutput` contenant le booléen `enabled` et l'horodatage.
// @Description  - Persistence guarantees: Lecture pure en mémoire RAM.
// @Description  - Side effects: Aucun effet de bord.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing or expired JWT:**
// @Description    - Trigger: Jeton d'authentification absent, expiré ou corrompu.
// @Description    - Execution stage: Middleware JWT / Contexte d'authentification.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Insufficient privileges:**
// @Description    - Trigger: L'utilisateur ne possède pas le grade requis.
// @Description    - Execution stage: Contrôle des habilitations d'accès.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Success      200  {object}  admin_models.AdminDelegationSwitchOutput
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Router       /admin/moderation/delegation-switch [get]
func GetDelegationSwitchHandler(c *gin.Context) {
	enabled := admin_service.GetDelegationSwitchService()
	c.JSON(http.StatusOK, admin_models.AdminDelegationSwitchOutput{
		Enabled:   enabled,
		UpdatedAt: 0,
	})
}
