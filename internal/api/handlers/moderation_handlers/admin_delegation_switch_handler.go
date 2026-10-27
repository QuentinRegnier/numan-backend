package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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
// @Success      200  {object}  moderation_models.AdminDelegationSwitchOutput
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Router       /admin/moderation/delegation-switch [get]
func GetDelegationSwitchHandler(c *gin.Context) {
	enabled := moderation_service.GetDelegationSwitchService(c.Request.Context())
	c.JSON(http.StatusOK, moderation_models.AdminDelegationSwitchOutput{
		Enabled:   enabled,
		UpdatedAt: 0,
	})
}

// SetDelegationSwitchHandler godoc
// @Summary      Modifier l'interrupteur de délégation administrative (Administrateur uniquement)
// @Description  Permet exclusivement à un Administrateur (Grade 4) d'activer ou de désactiver la délégation des contradictions vers les Super-Modérateurs. La valeur est enregistrée à chaud en mémoire et appliquée immédiatement à toutes les nouvelles contradictions survenant dans le système de modération.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `enabled` (boolean) dans le corps JSON (`moderation_models.AdminDelegationSwitchInput`).
// @Description  - Validation rules: Corps JSON structurellement valide obligatoire.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité de l'administrateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Contrôle d'accès strict : vérifie que `user_grade == UserGradeAdmin` (4). Rejet 403 immédiat sinon.
// @Description  3. Désérialisation et validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  4. Mutation atomique de la variable globale `ModerationAdminDelegationEnabled` via `moderation_service.SetDelegationSwitchService`.
// @Description  5. Capture de l'horodatage de modification en millisecondes.
// @Description  6. Renvoi du DTO de confirmation `moderation_models.AdminDelegationSwitchOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.AdminDelegationSwitchOutput`.
// @Description  - Persistence guarantees: Mise à jour en mémoire vive de l'instance d'exécution.
// @Description  - Side effects: Les prochaines contradictions de revue par les pairs basculent instantanément vers la nouvelle file d'attente cible.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload:**
// @Description    - Trigger: Le corps JSON est malformé ou le champ `enabled` est manquant.
// @Description    - Execution stage: Validation du corps de requête (`c.ShouldBindJSON`).
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
// @Description  - **[INTERNAL_SERVER_ERROR] Internal configuration mutation failure:**
// @Description    - Trigger: Erreur interne inattendue lors de l'application de la bascule.
// @Description    - Execution stage: Mutation dans `SetDelegationSwitchService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                                true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                                true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                                true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.AdminDelegationSwitchInput          true "Nouvel état de l'interrupteur"
// @Success      200  {object}  moderation_models.AdminDelegationSwitchOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/delegation-switch [post]
func SetDelegationSwitchHandler(c *gin.Context) {
	_, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")
	if grade != variables.UserGradeAdmin && grade != variables.MemberRoleAdmin {
		numan_error.RespondWithError(c, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil))
		return
	}

	var input moderation_models.AdminDelegationSwitchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	out, errSvc := moderation_service.SetDelegationSwitchService(c.Request.Context(), grade, input.Enabled)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
