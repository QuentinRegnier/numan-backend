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

// BlockHandler godoc
// @Summary      Bloquer un utilisateur
// @Description  Permet à l'utilisateur courant de bloquer un utilisateur cible.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: L'utilisateur doit être authentifié (Token valide).
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `TargetID` (l'identifiant de la cible).
// @Description  - Validation rules: Il est interdit de se bloquer soi-même.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Vérification du payload JSON (`relation_models.RelationActionInput`).
// @Description  2. **Extraction du Caller** : Récupération de l'ID via le contexte.
// @Description  3. **Règle métier (Auto-action)** : Rejet immédiat si `CallerID == TargetID`.
// @Description  4. **Lecture Cache L1** : Consultation de l'état actuel de la relation en O(1) RAM.
// @Description  5. **Idempotence** : Coupe-circuit si l'utilisateur est déjà dans l'état demandé.
// @Description  6. **Mise à jour Cache L1** : Application de l'action `variables.ActionBlockUser`. Enregistrement du nouvel état (état -1 pour bloqué). Retrait automatique des amis/followers.
// @Description  7. **Purge des recommandations** : Suppression des Cuckoo Filters et des flux personnalisés (Redis) pour empêcher la cible d'apparaître à l'avenir.
// @Description  8. **Persistance Asynchrone (Write-Behind)** : Mise en file d'attente (Redis EnqueueDB) de la transition pour synchronisation avec Postgres/Mongo.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Message de confirmation générique.
// @Description  - Persistence guarantees: Mise à jour immédiate en RAM L1. Écriture différée via Queue.
// @Description  - Side effects: Purge des Cuckoo Filters et Feeds personnalisés, rupture des liens amis/followers existants.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_JSON] Erreur de parsing:**
// @Description    - Trigger: Le corps JSON est mal formé ou ne respecte pas le type attendu.
// @Description    - Execution stage: Validation GIN (`ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Corps de requête invalide.").
// @Description    - Error code: `INVALID_JSON` (chaîne explicite) ou `numan_error.CodeInvalidPayload`.
// @Description
// @Description  - **[INVALID_PAYLOAD] Auto-action impossible:**
// @Description    - Trigger: Le `CallerID` est identique au `TargetID`.
// @Description    - Execution stage: Traitement métier (Étape 3).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Vous ne pouvez pas vous bloquer vous-même.").
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec Cache L1:**
// @Description    - Trigger: `UpdateRelationState` échoue lors de la mise à jour immédiate.
// @Description    - Execution stage: Mise à jour Cache L1 (Étape 6).
// @Description    - Response: `numan_error.PublicErrorResponse` générique.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Description
// @Description  - **[INTERNAL_ERROR] Échec de File d'Attente (Write-Behind):**
// @Description    - Trigger: Le système Redis n'arrive pas à mettre en attente la modification (`EnqueueDB`).
// @Description    - Execution stage: Persistance asynchrone (Étape 8).
// @Description    - Response: `numan_error.PublicErrorResponse` générique.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         relations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   relation_models.RelationActionInput true "Payload pour bloquer un utilisateur"
// @Success      200  {object}  map[string]interface{} "Confirmation"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /block/set [post]
func BlockHandler(c *gin.Context) {
	handleBlockAction(c, variables.ActionBlockUser)
}

// UnBlockHandler godoc
// @Summary      Débloquer un utilisateur
// @Description  Permet à l'utilisateur courant de débloquer un utilisateur cible.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: L'utilisateur doit être authentifié (Token valide).
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `TargetID` (l'identifiant de la cible).
// @Description  - Validation rules: Interdit de cibler son propre compte.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Vérification du payload JSON (`relation_models.RelationActionInput`).
// @Description  2. **Extraction du Caller** : Récupération de l'ID via le contexte.
// @Description  3. **Règle métier (Auto-action)** : Rejet immédiat si `CallerID == TargetID`.
// @Description  4. **Lecture Cache L1** : Consultation de l'état actuel de la relation en O(1) RAM.
// @Description  5. **Idempotence** : Coupe-circuit si l'utilisateur est déjà dans l'état demandé.
// @Description  6. **Mise à jour Cache L1** : Application de l'action `variables.ActionUnblockUser`. Enregistrement du nouvel état (état 0 pour neutre/none).
// @Description  7. **Persistance Asynchrone (Write-Behind)** : Mise en file d'attente (Redis EnqueueDB) de la transition pour synchronisation avec Postgres/Mongo.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Message de confirmation générique.
// @Description  - Persistence guarantees: Mise à jour immédiate en RAM L1. Écriture différée via Queue.
// @Description  - Side effects: Rétablissement de la possibilité d'interagir (état neutre).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_JSON] Erreur de parsing:**
// @Description    - Trigger: Le corps JSON est mal formé ou ne respecte pas le type attendu.
// @Description    - Execution stage: Validation GIN (`ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Corps de requête invalide.").
// @Description    - Error code: `INVALID_JSON` (chaîne explicite) ou `numan_error.CodeInvalidPayload`.
// @Description
// @Description  - **[INVALID_PAYLOAD] Auto-action impossible:**
// @Description    - Trigger: Le `CallerID` est identique au `TargetID`.
// @Description    - Execution stage: Traitement métier (Étape 3).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Vous ne pouvez pas effectuer cette action sur vous-même.").
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec Cache L1:**
// @Description    - Trigger: `UpdateRelationState` échoue lors de la mise à jour immédiate.
// @Description    - Execution stage: Mise à jour Cache L1 (Étape 6).
// @Description    - Response: `numan_error.PublicErrorResponse` générique.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Description
// @Description  - **[INTERNAL_ERROR] Échec de File d'Attente (Write-Behind):**
// @Description    - Trigger: Le système Redis n'arrive pas à mettre en attente la modification (`EnqueueDB`).
// @Description    - Execution stage: Persistance asynchrone (Étape 7).
// @Description    - Response: `numan_error.PublicErrorResponse` générique.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         relations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   relation_models.RelationActionInput true "Payload pour débloquer un utilisateur"
// @Success      200  {object}  map[string]interface{} "Confirmation"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /block/delete [post]
func UnBlockHandler(c *gin.Context) {
	handleBlockAction(c, variables.ActionUnblockUser)
}

func handleBlockAction(c *gin.Context, action bool) {
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

	if err := relation_service.ToggleBlock(c.Request.Context(), callerID, input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	message := "UnBlock"
	if action {
		message = "Block"
	}
	c.JSON(http.StatusOK, gin.H{"message": "Action '" + message + "' exécutée avec succès"})
}
