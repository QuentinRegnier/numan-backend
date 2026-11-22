package conversation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
	"github.com/gin-gonic/gin"
)

// UpdateConversationHandler godoc
// @Summary      Modifier une conversation ou ses métadonnées
// @Description  Met à jour le titre, les règles (`settings`) et, exclusivement pour les communautés publiques (`type = 3`), la description, l'avatar, le lien externe et la langue (`lang`). Toute modification sur un Message Privé (`type = 1`) ou toute tentative de renseigner des attributs de communauté publique sur un groupe privé (`type = 2`) est rejetée en `403 Forbidden`. Un changement de langue sur une communauté publique réinitialise atomiquement son éventuel `pin_slot` en L3/L1 et migre son score de recommandation vers le ZSET de la nouvelle langue.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`) et une signature HMAC valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: L'utilisateur appelant doit être membre actif de la conversation avec au moins le rôle Administrateur (`MemberRoleAdmin = 1`) ou Propriétaire (`MemberRoleOwner = 2`), vérifié en cascade L1 → L2 → L3 par `security_service.LeftConversation`.
// @Description  - Relevant middleware: `MaxBodySize`, `RateLimiter`, `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `PUT` (tous les paramètres métier dans le corps JSON).
// @Description  - Required fields (`conversation_models.UpdateConversationInput`) :
// @Description    - `conversation_id` (`int64`, `binding:"required"`) : identifiant strictement positif (`> 0`) de la conversation cible.
// @Description  - Optional fields:
// @Description    - `title` (`string`, `max=100` runes) : nouveau titre de la conversation.
// @Description    - `settings` (`conversation_models.ConversationSettings`) : nouveaux paramètres (mode d'écriture `write_permission`, approbation d'adhésion `join_approval_required`, etc.).
// @Description    - `description` (`string`, `<= 500` runes) : réservé aux communautés publiques (`type = 3`).
// @Description    - `avatar_id` (`int64`, `>= 0`) : réservé aux communautés publiques (`type = 3`).
// @Description    - `external_link` (`models.ExternalLinks`) : lien externe validé, réservé aux communautés publiques (`type = 3`).
// @Description    - `lang` (`string`, optionnel) : code langue ISO 639-1 supporté (`fr`, `en`, `es`, `it`, `pt`, `de`, `nl`, `ru`, `ar`), réservé aux communautés publiques (`type = 3`). S'il est omis ou vide (`""`), la langue et l'éventuel `pin_slot` existants sont préservés intacts.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction d'identité et binding JSON (Handler) :**
// @Description     - Extraction de `callerID` via `pkg.GetUserIDFromContext(c)`. Rejet `401` (`CodeUserIsNotIdentified`) si absent.
// @Description     - Binding GIN via `c.ShouldBindJSON(&input)`.
// @Description  2. **Validation métier et contrôle Zero-Trust (`conversation_service.UpdateConversation`) :**
// @Description     - Validation du DTO (`input.Validate()` : `conversation_id > 0`, `title <= 100`, `description <= 500`, `avatar_id >= 0`, `lang` supporté si non vide, `settings.Validate()`, `external_link.Validate()`).
// @Description     - Appel de `security_service.LeftConversation(ctx, conversationID, callerID)` (cascade L1 → L2 → L3) : rejette `404` si introuvable ou archivée (`state = 1`), `403` si non-membre ou rôle `< MemberRoleAdmin`.
// @Description  3. **Contrôles métiers selon le type de conversation :**
// @Description     - Rejet `403` si la conversation est un Message Privé (`type = 1`).
// @Description     - Sur un groupe privé (`type = 2`) : rejet `403` si `description`, `avatar_id`, `external_link` ou `lang` est fourni/modifié.
// @Description     - Sur une communauté publique (`type = 3`) : application des mutations ; si `lang` change effectivement, mise à `0` de `PinSlot`.
// @Description  4. **Synchronisation atomique L3 en cas de changement de langue :**
// @Description     - Si `hasLangChanged` et `PostgresDB != nil` : exécution synchrone de `postgres.FuncUpdateCommunityLang` (`messaging.func_update_community_lang`) qui persiste `lang` et remet `pin_slot = NULL` atomiquement.
// @Description  5. **Mise à jour immédiate L1 et réconciliation Recommandation Communautés :**
// @Description     - Écrasement dans L1 Object Cache (`SetConversationInObjectCache`) et dans `ConvMeta` (via `service.ToConvLiteRequest`, conservant `Lang`, `IsPartner`, `PinSlot`, `Mode`).
// @Description     - Si `hasLangChanged` : purge de l'éventuel épinglage dans `community:reco:pins:{old_lang}` (`DeleteCommunityPinInCache` + `RemoveCommunityPinByConversationID`) et migration du score ZSET de `community:reco:ranked:{old_lang}` vers `community:reco:ranked:{new_lang}` (ou calcul du score initial si absent).
// @Description  6. **Persistance Write-Behind et tâches de fond :**
// @Description     - Enqueue dans `redis.EnqueueDB` (`EntityConversation`, `ActionUpdate`, `TargetAll`). Rejet `500` (`CodeInternalError`) en cas d'échec.
// @Description     - En goroutine : diffusion WebSocket `conversation.updated`, enregistrement Sync Ledger (`RecordConversationMutation`), et si `join_approval_required` vient d'être désactivé sur une communauté publique, acceptation massive des membres en attente (`acceptAllPendingMembers`, incluant hooks `user:public_comms` + croissance 7j).
// @Description     - Marquage `TouchInboxActivity(callerID)` et réponse `200 OK`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: JSON `conversation_models.UpdateConversationOutput` (`inbox_update_at`).
// @Description  - Persistence guarantees: Mise à jour L1 synchrone (Object Cache, `ConvMeta`, et migration pins/ZSET en cas de changement de `lang`), mutation L3 synchrone sur changement de `lang`, durabilité L2/L3 asynchrone via Write-Behind, diffusion temps réel WebSocket.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] JSON malformé ou contraintes de champs violées:**
// @Description    - Trigger: Corps JSON invalide, `conversation_id` manquant ou `<= 0`, `title > 100` runes, `description > 500` runes, `avatar_id < 0`, `settings` ou `external_link` invalides.
// @Description    - Execution stage: Binding GIN (`c.ShouldBindJSON`) ou validation métier (`UpdateConversationInput.Validate` / `ConversationPayload.Validate`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Langue non supportée:**
// @Description    - Trigger: `lang` non vide absent de la whitelist ISO 639-1 (`fr`, `en`, `es`, `it`, `pt`, `de`, `nl`, `ru`, `ar`), ex: `"jp"`, `"un"`, `"fr-FR"`.
// @Description    - Execution stage: `UpdateConversationInput.Validate` / `variables.NormalizeCommunityLang`.
// @Description    - Response: `numan_error.PublicErrorResponse` (`"La langue spécifiée n'est pas supportée."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[numan_error.CodeUnauthorized] Jeton JWT ou signature HMAC invalide:**
// @Description    - Trigger: En-tête `Authorization` absent/expiré/invalide ou signature HMAC invalide.
// @Description    - Execution stage: Middlewares `JWTMiddleware` / `HMACMiddleware`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (`"UNAUTHORIZED"`).
// @Description
// @Description  - **[numan_error.CodeUserIsNotIdentified] Utilisateur non identifié dans le contexte:**
// @Description    - Trigger: `userID` absent du contexte Gin.
// @Description    - Execution stage: Extraction d'identité dans le handler (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Utilisateur non identifié."`).
// @Description    - Error code: `numan_error.CodeUserIsNotIdentified` (`"USER_NOT_IDENTIFIED"`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[numan_error.CodeForbidden] Membre non autorisé ou rôle insuffisant:**
// @Description    - Trigger: L'appelant n'est pas membre actif de la conversation ou possède un rôle inférieur à Administrateur (`role < MemberRoleAdmin`).
// @Description    - Execution stage: Contrôle Zero-Trust `security_service.LeftConversation`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (`"FORBIDDEN_ACCESS"`).
// @Description
// @Description  - **[numan_error.CodeForbidden] Type de conversation incompatible avec la modification:**
// @Description    - Trigger: La cible est un Message Privé (`type = 1`), ou l'appelant tente de renseigner `description`, `avatar_id`, `external_link` ou `lang` sur un groupe privé classique (`type = 2`).
// @Description    - Execution stage: Contrôles métiers dans `conversation_service.UpdateConversation`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (`"FORBIDDEN_ACCESS"`).
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[numan_error.CodeNotFound] Conversation introuvable ou archivée:**
// @Description    - Trigger: `conversation_id` n'existe dans aucune couche L1/L2/L3 ou la conversation est archivée (`state = 1`).
// @Description    - Execution stage: Contrôle Zero-Trust `security_service.LeftConversation`.
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Conversation introuvable ou archivée."`).
// @Description    - Error code: `numan_error.CodeNotFound` (`"RESOURCE_NOT_FOUND"`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeDatabaseError] Échec PostgreSQL L3 (chargement ou changement de langue):**
// @Description    - Trigger: Erreur SQL lors de la cascade L3 dans `LeftConversation` ou lors de l'appel atomique `postgres.FuncUpdateCommunityLang` pendant un changement de langue.
// @Description    - Execution stage: `security_service.LeftConversation` ou étape 2B de `UpdateConversation`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeDatabaseError` (`"DATABASE_ERROR"`).
// @Description
// @Description  - **[numan_error.CodeInternalError] Échec de mise en file Write-Behind:**
// @Description    - Trigger: Indisponibilité Redis lors de l'appel à `redis.EnqueueDB` (`ActionUpdate`).
// @Description    - Execution stage: Étape 4 de `conversation_service.UpdateConversation`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (`"INTERNAL_SERVER_ERROR"`).
// @Tags         conversations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   conversation_models.UpdateConversationInput true "Payload de modification de la conversation (lang optionnel pour communautés publiques)"
// @Success      200  {object}  conversation_models.UpdateConversationOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (JSON invalide, champs hors bornes ou langue non supportée)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized (JWT/HMAC invalide ou utilisateur non identifié)"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Rôle insuffisant, MP non modifiable ou champs communauté sur groupe privé)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (Conversation introuvable ou archivée)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Database / Internal Server Error"
// @Router       /conversation/update [put]
func UpdateConversationHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input conversation_models.UpdateConversationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	output, err := conversation_service.UpdateConversation(c.Request.Context(), callerID, input.ConversationID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, output)
}
