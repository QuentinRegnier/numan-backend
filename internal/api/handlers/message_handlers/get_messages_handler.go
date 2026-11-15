package message_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/gin-gonic/gin"
)

// GetMessagesHandler godoc
// @Summary      Récupérer l'historique des messages
// @Description  Renvoie l'historique paginé des messages d'une conversation avec hydratation complète (avatars, pseudos, compteurs de réactions et interaction utilisateur).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT d'authentification valide.
// @Description  - Required permissions or roles: L'utilisateur appelant doit avoir un rôle suffisant (`MemberRoleNormal` minimum) dans la conversation.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation et Extraction de l'Identité :**
// @Description     - Récupération du `callerID` depuis le contexte HTTP fourni par le middleware d'authentification.
// @Description     - Binding du payload de pagination (`conversation_id`, `offset_id`, `limit`, `direction`).
// @Description  2. **Contrôle de Sécurité Zero-Trust :**
// @Description     - Appel de `security_service.LeftMember` pour s'assurer que l'appelant possède au moins le rôle `MemberRoleNormal` dans la conversation.
// @Description  3. **Récupération de la Conversation (Cascade) :**
// @Description     - Tentative de lecture depuis le cache objet L1.
// @Description     - Si Cache Miss, lecture depuis Mongo (L2), puis Postgres (L3).
// @Description     - En cas de recours à L3, une opération asynchrone (`redis.EnqueueDB`) est déclenchée pour l'auto-guérison de Mongo, et le cache L1 est synchronisé.
// @Description  4. **Résolution d'Index et Chargement Massif :**
// @Description     - Résolution des identifiants des messages via `cache_service.GetMessageIDsFromSpeedCache` (gestion de l'Offset et de la Direction).
// @Description     - Hydratation massive (MGET) des payloads de messages via `object_cache_service.GetMessagesView`.
// @Description  5. **Filtrage, Assemblage et Hydratation :**
// @Description     - Filtrage conditionnel des messages systèmes si la conversation exige leur masquage (`HideSystemMessages`).
// @Description     - Pour chaque message : génération du lien HMAC de l'avatar de l'expéditeur (ou récupération de l'ID communautaire mode Twitch).
// @Description     - Récupération O(1) en RAM des compteurs de réactions et de la réaction spécifique de l'utilisateur.
// @Description     - Renvoi des vues finales agrégées.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Requête mal formatée ou paramètres invalides:**
// @Description    - Trigger: Paramètres de pagination invalides ou ID de conversation manquant.
// @Description    - Execution stage: Validation GIN.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`
// @Description
// @Description  🟡 **401 Unauthorized:**
// @Description
// @Description  - **[numan_error.CodeUnauthorized] Jeton invalide ou absent:**
// @Description    - Trigger: Le client HTTP n'envoie pas de token, ou le JWT a expiré.
// @Description    - Execution stage: Middleware global d'authentification.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[numan_error.CodeForbidden] Accès refusé:**
// @Description    - Trigger: L'utilisateur ne fait pas partie de la conversation ou son rôle est insuffisant (banni ou en attente).
// @Description    - Execution stage: Évaluation Zero-Trust par `security_service.LeftMember`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[numan_error.CodeNotFound] Ressource introuvable:**
// @Description    - Trigger: La conversation ciblée est introuvable après passage complet L1 -> L2 -> L3.
// @Description    - Execution stage: Requête ultime dans Postgres (L3).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur de service inattendue:**
// @Description    - Trigger: Défaut majeur sur Redis ou MongoDB rendant l'hydratation massive impossible.
// @Description    - Execution stage: Exécution profonde du service métier.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`
// @Tags         messages
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   message_models.GetMessagesInput true "Payload pour récupérer l'historique des messages d'une conversation spécifique"
// @Success      200  {object}  message_models.GetMessagesOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /messages/get [post]
func GetMessagesHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input message_models.GetMessagesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	messages, err := message_service.GetMessages(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, message_models.GetMessagesOutput{Messages: messages})
}
