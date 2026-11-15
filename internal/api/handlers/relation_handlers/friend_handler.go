package relation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/relation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// FriendHandler godoc
// @Summary      Mettre un utilisateur en ami
// @Description  Permet à un utilisateur d'ajouter un utilisateur en ami.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Requiert un utilisateur authentifié.
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `TargetID`.
// @Description  - Validation rules: Interdit de s'ajouter soi-même.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Lecture du JSON (`relation_models.RelationActionInput`).
// @Description  2. **Extraction Caller** : Vérification du token et identifiant via le contexte.
// @Description  3. **Contrôle d'intégrité** : Rejet automatique si `CallerID == TargetID`.
// @Description  4. **Lecture du Cache L1** : Extraction de l'état actuel entre les deux utilisateurs.
// @Description  5. **Contrôle de blocage** : Rejet immédiat avec 403 si l'état est "bloqué".
// @Description  6. **Transition d'état** : Application de l'action `variables.ActionPromoteToFriend`. Promotion de la relation depuis "follow" vers "friend" (état 2), ou création pure (ActionCreate).
// @Description  7. **Mise à jour L1** : Application instantanée de l'état en mémoire.
// @Description  8. **Mise en file d'attente (Redis)** : Enregistrement de l'évènement pour persistance en BDD.
// @Description  9. **Notifications (Asynchrone)** : Lancement d'une goroutine (DispatchNotification) pour notifier la cible de l'ajout en ami.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Message de confirmation de succès.
// @Description  - Persistence guarantees: L1 synchrone, BDD asynchrone via Queue.
// @Description  - Side effects: Notification d'amitié déclenchée.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_JSON] Format incorrect:**
// @Description    - Trigger: Le payload JSON ne passe pas le binding GIN.
// @Description    - Execution stage: Validation de la requête.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Corps de requête invalide.").
// @Description    - Error code: `INVALID_JSON` (ou `numan_error.CodeInvalidPayload`).
// @Description
// @Description  - **[INVALID_PAYLOAD] Auto-amitié interdite:**
// @Description    - Trigger: L'utilisateur cible son propre ID.
// @Description    - Execution stage: Règle métier.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Vous ne pouvez pas être ami avec vous-même.").
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN] Utilisateur bloqué:**
// @Description    - Trigger: Relation existante bloquée (état -1).
// @Description    - Execution stage: Lecture de l'état actuel.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Action impossible : Utilisateur bloqué.").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec L1:**
// @Description    - Trigger: Problème d'écriture dans le cache RAM L1.
// @Description    - Execution stage: Mise à jour immédiate.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Description
// @Description  - **[INTERNAL_ERROR] Échec de Persistance:**
// @Description    - Trigger: Impossible de joindre Redis EnqueueDB.
// @Description    - Execution stage: Write-Behind asynchrone.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         relations
// @Accept       json
// @Produce      json
// @Param        Authorization header string false "Bearer <Current_JWT> (Optional)"
// @Param        X-Signature   header string true  "HMAC calculated with OLD MasterToken"
// @Param        X-Timestamp   header string true  "Unix Timestamp"
// @Param        input         body   relation_models.RelationActionInput true "Payload pour mettre un utilisateur en ami"
// @Success      200  {object}  map[string]interface{} "Confirmation"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Action impossible"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /friend/set [post]
func FriendHandler(c *gin.Context) {
	handleFriendAction(c, variables.ActionPromoteToFriend)
}

// UnFriendHandler godoc
// @Summary      Supprimer un utilisateur de ses amis
// @Description  Permet à un utilisateur de supprimer un utilisateur de ses amis.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Requiert un utilisateur authentifié.
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `TargetID`.
// @Description  - Validation rules: Interdit de cibler son propre compte.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Lecture du JSON (`relation_models.RelationActionInput`).
// @Description  2. **Extraction Caller** : Vérification du token et identifiant via le contexte.
// @Description  3. **Contrôle d'intégrité** : Rejet automatique si `CallerID == TargetID`.
// @Description  4. **Lecture du Cache L1** : Extraction de l'état actuel entre les deux utilisateurs.
// @Description  5. **Contrôle de blocage** : Rejet immédiat avec 403 si l'état est "bloqué".
// @Description  6. **Transition d'état** : Application de l'action `variables.ActionDemoteToFollower`. Rétrogradation stricte de la relation depuis "friend" vers "follow" (état 1).
// @Description  7. **Mise à jour L1** : Application instantanée de l'état en mémoire.
// @Description  8. **Mise en file d'attente (Redis)** : Enregistrement de l'évènement pour persistance en BDD.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Message de confirmation de succès.
// @Description  - Persistence guarantees: L1 synchrone, BDD asynchrone via Queue.
// @Description  - Side effects: Aucun (les suppressions d'amis ne déclenchent pas de notification).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_JSON] Format incorrect:**
// @Description    - Trigger: Le payload JSON ne passe pas le binding GIN.
// @Description    - Execution stage: Validation de la requête.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Corps de requête invalide.").
// @Description    - Error code: `INVALID_JSON` (ou `numan_error.CodeInvalidPayload`).
// @Description
// @Description  - **[INVALID_PAYLOAD] Auto-ciblage interdit:**
// @Description    - Trigger: L'utilisateur cible son propre ID.
// @Description    - Execution stage: Règle métier.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Vous ne pouvez pas effectuer cette action sur vous-même.").
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN] Utilisateur bloqué:**
// @Description    - Trigger: Relation existante bloquée (état -1).
// @Description    - Execution stage: Lecture de l'état actuel.
// @Description    - Response: `numan_error.PublicErrorResponse` ("Action impossible : Utilisateur bloqué.").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec L1:**
// @Description    - Trigger: Problème d'écriture dans le cache RAM L1.
// @Description    - Execution stage: Mise à jour immédiate.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Description
// @Description  - **[INTERNAL_ERROR] Échec de Persistance:**
// @Description    - Trigger: Impossible de joindre Redis EnqueueDB.
// @Description    - Execution stage: Write-Behind asynchrone.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         relations
// @Accept       json
// @Produce      json
// @Param        Authorization header string false "Bearer <Current_JWT> (Optional)"
// @Param        X-Signature   header string true  "HMAC calculated with OLD MasterToken"
// @Param        X-Timestamp   header string true  "Unix Timestamp"
// @Param        input         body   relation_models.RelationActionInput true "Payload pour supprimer un utilisateur de ses amis"
// @Success      200  {object}  map[string]interface{} "Confirmation"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Action impossible"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /friend/delete [post]
func UnFriendHandler(c *gin.Context) {
	handleFriendAction(c, variables.ActionDemoteToFollower)
}

func handleFriendAction(c *gin.Context, action bool) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input relation_models.RelationActionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}
	input.Action = action

	if err := relation_service.ToggleFriend(c.Request.Context(), callerID, input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	message := "DemoteToFollower"
	if action == variables.ActionPromoteToFriend {
		message = "PromoteToFriend"
	}
	c.JSON(http.StatusOK, gin.H{"message": "Action '" + message + "' exécutée avec succès"})
}
