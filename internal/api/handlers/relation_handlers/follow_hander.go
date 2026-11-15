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

// FollowHandler godoc
// @Summary      S'abonner à un utilisateur
// @Description  Permet à l'utilisateur courant de s'abonner à un utilisateur cible.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Utilisateur authentifié.
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `TargetID`.
// @Description  - Validation rules: Impossible de s'abonner à soi-même.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Binding JSON de `relation_models.RelationActionInput`.
// @Description  2. **Extraction Caller** : Récupération de l'identité de l'appelant.
// @Description  3. **Vérification métier préliminaire** : Rejet de l'action si `CallerID == TargetID`.
// @Description  4. **Contrôle du Blocage (O(1) L1)** : Vérification de l'état actuel pour rejeter si la relation est bloquée (état -1).
// @Description  5. **Idempotence** : Si l'état actuel correspond déjà à la demande (ex: déjà abonné ou ami), coupe-circuit instantané (zéro I/O DB).
// @Description  6. **Mise à jour Cache L1** : Application de l'action `variables.ActionSubcribeUser`. Mise à jour en mémoire du nouvel état (état 1 pour follow).
// @Description  7. **Persistance Asynchrone** : Insertion dans la queue Redis via `EnqueueDB` pour write-behind.
// @Description  8. **Notifications** : Dispatch asynchrone d'une notification via `notification_service`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Message de confirmation générique.
// @Description  - Persistence guarantees: Cache L1 immédiat, persistance asynchrone Redis/DB.
// @Description  - Side effects: Génération d'une notification, impact sur les feeds (Fan-Out).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_JSON] Erreur de parsing:**
// @Description    - Trigger: Le corps ou type des paramètres est invalide.
// @Description    - Execution stage: Validation GIN (`ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Corps de requête invalide.").
// @Description    - Error code: `INVALID_JSON` (chaîne explicite) ou `numan_error.CodeInvalidPayload`.
// @Description
// @Description  - **[INVALID_PAYLOAD] Auto-abonnement impossible:**
// @Description    - Trigger: `CallerID == TargetID`.
// @Description    - Execution stage: Traitement métier (Étape 1).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Vous ne pouvez pas vous abonner à vous-même.").
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN] Utilisateur bloqué:**
// @Description    - Trigger: La relation actuelle est à l'état de blocage (-1).
// @Description    - Execution stage: Contrôle des droits et état de relation (Étape 2).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Action impossible : Utilisateur bloqué.").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec Cache L1:**
// @Description    - Trigger: L'enregistrement en mémoire (Cache L1) échoue.
// @Description    - Execution stage: Mise à jour cache.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Description
// @Description  - **[INTERNAL_ERROR] Échec de File d'Attente:**
// @Description    - Trigger: L'envoi de la commande via `redis.EnqueueDB` échoue.
// @Description    - Execution stage: Persistance asynchrone.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         relations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   relation_models.RelationActionInput true "Payload pour s'abonner à un utilisateur"
// @Success      200  {object}  map[string]interface{} "Confirmation"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Action impossible"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /follow/set [post]
func FollowHandler(c *gin.Context) {
	handleFollowAction(c, variables.ActionSubcribeUser)
}

// UnFollowHandler godoc
// @Summary      Se désabonner d'un utilisateur
// @Description  Permet à l'utilisateur courant de se désabonner d'un utilisateur cible.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Utilisateur authentifié.
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `TargetID`.
// @Description  - Validation rules: Interdit de cibler son propre compte.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Binding JSON de `relation_models.RelationActionInput`.
// @Description  2. **Extraction Caller** : Récupération de l'identité de l'appelant.
// @Description  3. **Vérification métier préliminaire** : Rejet de l'action si `CallerID == TargetID`.
// @Description  4. **Contrôle du Blocage (O(1) L1)** : Vérification de l'état actuel pour rejeter si la relation est bloquée (état -1).
// @Description  5. **Idempotence** : Si l'état actuel correspond déjà à la demande (ex: déjà non abonné), coupe-circuit instantané (zéro I/O DB).
// @Description  6. **Mise à jour Cache L1** : Application de l'action `variables.ActionUnSubcribeUser`. Mise à jour en mémoire du nouvel état (état 0 pour none).
// @Description  7. **Persistance Asynchrone** : Insertion dans la queue Redis via `EnqueueDB` pour write-behind.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Message de confirmation générique.
// @Description  - Persistence guarantees: Cache L1 immédiat, persistance asynchrone Redis/DB.
// @Description  - Side effects: Impact sur les feeds (Fan-Out). Aucune notification n'est envoyée pour un désabonnement.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_JSON] Erreur de parsing:**
// @Description    - Trigger: Le corps ou type des paramètres est invalide.
// @Description    - Execution stage: Validation GIN (`ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Corps de requête invalide.").
// @Description    - Error code: `INVALID_JSON` (chaîne explicite) ou `numan_error.CodeInvalidPayload`.
// @Description
// @Description  - **[INVALID_PAYLOAD] Auto-désabonnement impossible:**
// @Description    - Trigger: `CallerID == TargetID`.
// @Description    - Execution stage: Traitement métier (Étape 1).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Vous ne pouvez pas effectuer cette action sur vous-même.").
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN] Utilisateur bloqué:**
// @Description    - Trigger: La relation actuelle est à l'état de blocage (-1).
// @Description    - Execution stage: Contrôle des droits et état de relation (Étape 2).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Action impossible : Utilisateur bloqué.").
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec Cache L1:**
// @Description    - Trigger: L'enregistrement en mémoire (Cache L1) échoue.
// @Description    - Execution stage: Mise à jour cache.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Description
// @Description  - **[INTERNAL_ERROR] Échec de File d'Attente:**
// @Description    - Trigger: L'envoi de la commande via `redis.EnqueueDB` échoue.
// @Description    - Execution stage: Persistance asynchrone.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         relations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   relation_models.RelationActionInput true "Payload pour se désabonner d'un utilisateur"
// @Success      200  {object}  map[string]interface{} "Confirmation"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Action impossible"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /follow/delete [post]
func UnFollowHandler(c *gin.Context) {
	handleFollowAction(c, variables.ActionUnSubcribeUser)
}

func handleFollowAction(c *gin.Context, action bool) {
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

	if err := relation_service.ToggleFollow(c.Request.Context(), callerID, input); err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	message := "UnSubscribe"
	if action {
		message = "Subscribe"
	}
	c.JSON(http.StatusOK, gin.H{"message": "Action '" + message + "' exécutée avec succès"})
}
