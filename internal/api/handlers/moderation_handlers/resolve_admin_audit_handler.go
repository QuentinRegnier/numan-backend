package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ResolveAdminAuditHandler godoc
// @Summary      Statuer sur l'audit d'un quota de 3 blâmes (Administrateur uniquement)
// @Description  Permet exclusivement à un Administrateur (Grade 4) de statuer sur le Super-Report d'audit déclenché automatiquement au 3ème blâme cumulé par un utilisateur. La décision arbitrale tranche entre la validation des blâmes (`validate`) ou leur révocation (`revoke`). La validation prononce un bannissement définitif (Hard Ban) et gratifie les modérateurs ayant instruit les blâmes (+0.02 de $T_{\text{mod}}$). La révocation réhabilite l'utilisateur cible (bonus de contestation `successful_appeal` à +0.08, suppression des blâmes injustifiés) et applique une pénalité sévère (-0.25) aux seuls modérateurs des blâmes révoqués, avec rétrogradation automatique au rang standard si leur fiabilité tombe sous 0.50.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le requérant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64), `decision` (`"validate"` ou `"revoke"`), et `rationale` (string non vide) dans `moderation_models.ResolveAdminAuditInput`.
// @Description  - Optional fields: `revoked_blame_report_ids` ([]int64, liste des identifiants des blâmes spécifiques à révoquer en cas de pardon partiel).
// @Description  - Validation rules: `report_id` > 0 et `decision` strictement restreinte à `"validate"` ou `"revoke"`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité de l'administrateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Contrôle d'accès strict : vérifie que `user_grade == UserGradeAdmin` (4). Rejet 403 immédiat sinon.
// @Description  3. Validation et parsing de la charge utile JSON (`c.ShouldBindJSON`).
// @Description  4. Récupération du ticket dans le cache Redis L1 (`cache_service.GetModerationTicket`).
// @Description  5. Contrôle d'état : vérifie que le ticket est un audit d'escalade en attente (`ReportStateEscalated = 4` et `escalation_type == "blame_quota_audit"`).
// @Description  6. Retrait du ticket de la file d'attente administrative (`ModerationQueueAdmin`).
// @Description  7. Traitement selon la décision :
// @Description     - **Si `validate`** :
// @Description       a. Exécution du Hard Ban définitif sur la cible (`ExecuteAdminHardBanService`).
// @Description       b. Gratification réputationnelle (+0.02) de l'ensemble des modérateurs (proposants et validateurs) ayant instruit les blâmes.
// @Description       c. Clôture avec statut `blames_validated_fatal_ban`.
// @Description     - **Si `revoke`** :
// @Description       a. Détermination du sous-ensemble de blâmes révoqués (total ou partiel via `RevokedBlameReportIDs`).
// @Description       b. Réajustement immédiat de l'historique et du compteur de blâmes de la cible (`targetStats.BlameCount`).
// @Description       c. Émission d'un bonus `TrustTriggerSuccessfulAppeal` (+0.08) au profit de l'utilisateur cible réhabilité.
// @Description       d. Application d'une pénalité de -0.25 sur le score de fiabilité $T_{\text{mod}}$ des modérateurs associés aux seuls blâmes annulés.
// @Description       e. Si le score d'un modérateur tombe sous 0.50 (`ModTrustRevocationThreshold`), rétrogradation automatique immédiate au rang Utilisateur (`UserGradeUser = 0`) et purge du cache SPEED L1.
// @Description       f. Clôture avec statut `blames_revoked_pardon`.
// @Description  8. Sauvegarde du ticket au statut clos (`ReportStateClosed = -1`) et archivage PostgreSQL L3 (`postgres.FuncArchiveClosedReport`).
// @Description  9. Suppression du ticket de la RAM Redis.
// @Description  10. Renvoi du DTO de confirmation `moderation_models.ResolveAdminAuditOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ResolveAdminAuditOutput` récapitulant la cible bannie, les modérateurs récompensés, pénalisés ou rétrogradés.
// @Description  - Persistence guarantees: Archivage PostgreSQL ACID et synchronisation atomique des réputations modérateurs en RAM et base de données.
// @Description  - Side effects: Exécution du Hard Ban ou réhabilitation de la cible avec ajustement disciplinaire des modérateurs fautifs.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or invalid decision:**
// @Description    - Trigger: Décision absente ou différente de `"validate"` / `"revoke"`, ou ticket non configuré pour un audit de quota.
// @Description    - Execution stage: Validation du corps de requête (`c.ShouldBindJSON`) ou vérification du type d'escalade.
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
// @Description    - Trigger: L'utilisateur ne possède pas le grade Administrateur (4).
// @Description    - Execution stage: Contrôle des privilèges administratifs.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Ticket not found:**
// @Description    - Trigger: L'identifiant `report_id` n'existe pas dans le cache.
// @Description    - Execution stage: Recherche du ticket (`cache_service.GetModerationTicket`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Hard ban execution or database archiving failure:**
// @Description    - Trigger: Échec lors de la révocation des sessions, de l'archivage SQL ou de la mise à jour des statistiques de modération.
// @Description    - Execution stage: Exécution de la résolution d'audit.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                        true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                        true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                        true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ResolveAdminAuditInput      true "Décision d'audit (validate ou revoke) et justification"
// @Success      200  {object}  moderation_models.ResolveAdminAuditOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/audit/resolve [post]
func ResolveAdminAuditHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.ResolveAdminAuditInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou décision manquante (validate/revoke).", err))
		return
	}

	out, errSvc := moderation_service.ResolveAdminAuditService(c.Request.Context(), callerID, grade, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
