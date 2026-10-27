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

// AdminHardBanHandler godoc
// @Summary      Exécuter un bannissement définitif (Hard Ban immédiat)
// @Description  Permet exclusivement à un Administrateur (Grade 4) d'ordonner le bannissement définitif et irrévocable d'un utilisateur. Déclenche la mise à 0.00 du Trust Score, l'interdiction d'accès applicative, la révocation immédiate en RAM (tombstone) et en BDD de toutes les sessions actives (JWT / Refresh tokens), l'inscription sur la liste noire d'empreintes stéganographiques (`moderation.stegano_bans`), et la notification de clarification CGU.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le requérant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `target_user_id` (int64) et `reason` (string non vide) dans le corps JSON (`moderation_models.AdminHardBanInput`).
// @Description  - Optional fields: `report_id` (int64, identifiant du rapport d'origine éventuel) et `cgu_feedback_note` (string, note explicative transmise à la cible).
// @Description  - Validation rules: `target_user_id` strictement positif (`binding:"required,gt=0"`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité de l'administrateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Contrôle d'accès strict : vérifie que `user_grade == UserGradeAdmin` (4). Rejet 403 immédiat sinon.
// @Description  3. Validation et parsing du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  4. Délégation au service d'exécution (`moderation_service.ExecuteAdminHardBanService`).
// @Description  5. Publication de l'événement d'infraction majeure `variables.TrustTriggerMajorInfraction` (-1.00) dans la file Redis du Worker.
// @Description  6. Inscription immédiate de l'identifiant stéganographique dans le Set Redis (`moderation:stegano_banlist`) et persistance SQL (`moderation.func_register_stegano_ban`).
// @Description  7. Chargement de toutes les sessions actives de l'utilisateur (`postgres.FuncLoadAllUserSessions`).
// @Description  8. Pour chaque session : inscription en liste noire mémoire (`redis.SessionBlacklist`) avec TTL de 24h et émission d'un ordre de suppression asynchrone `redis.EnqueueDB` ciblant `TargetAll`.
// @Description  9. Mise à jour de l'état du profil utilisateur en mémoire L1 (`UsersLite`) : `TrustScore = 0.00`, `Banned = true`.
// @Description  10. Si un `report_id` est rattaché : transition du ticket en état clos (`ReportStateClosed = -1`), enregistrement de la note CGU et archivage PostgreSQL L3.
// @Description  11. Renvoi du DTO de confirmation `moderation_models.AdminHardBanOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.AdminHardBanOutput` confirmant le bannissement, le score forcé à 0.00 et le nombre de sessions révoquées.
// @Description  - Persistence guarantees: Révocation immédiate en RAM L1, inscription stéganographique persistée en base et purge de toutes les sessions.
// @Description  - Side effects: L'utilisateur banni est déconnecté instantanément de tous ses appareils et ses futurs uploads multimédias sont bloqués à la racine.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or missing target_user_id:**
// @Description    - Trigger: Le corps JSON est malformé ou `target_user_id` est manquant / négatif.
// @Description    - Execution stage: Validation du contrôleur Gin (`c.ShouldBindJSON`).
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
// @Description  - **[FORBIDDEN_ACCESS] Administrator privileges required:**
// @Description    - Trigger: L'appelant ne possède pas le grade Administrateur (4).
// @Description    - Execution stage: Contrôle des privilèges administratifs.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Target user not found:**
// @Description    - Trigger: L'identifiant utilisateur cible n'existe pas dans le système.
// @Description    - Execution stage: Vérification du compte cible.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Session revocation or database failure:**
// @Description    - Trigger: Échec de communication avec Redis ou PostgreSQL lors de la purge des sessions ou de l'enregistrement du ban stéganographique.
// @Description    - Execution stage: Exécution de la procédure de Hard Ban.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.AdminHardBanInput      true "Paramètres du Hard Ban (utilisateur cible, motif, note CGU)"
// @Success      200  {object}  moderation_models.AdminHardBanOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Target User Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/hard-ban [post]
func AdminHardBanHandler(c *gin.Context) {
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

	var input moderation_models.AdminHardBanInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	out, errSvc := moderation_service.ExecuteAdminHardBanService(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
