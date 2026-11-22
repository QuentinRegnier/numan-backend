package community_recommendation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/gin-gonic/gin"
)

// UnpinCommunityHandler godoc
// @Summary      Retirer l'épinglage d'une communauté publique (Admin)
// @Description  Permet exclusivement à un Administrateur (Grade 4) de retirer de manière atomique et idempotente le slot d'épinglage (`pin_slot = NULL`) d'une communauté publique (`type = 3`) en PostgreSQL L3, puis de synchroniser immédiatement le HASH Redis `community:reco:pins:{lang}`, les caches L1 (`Object Cache`, `ConvMeta`) et la file Write-Behind L2 MongoDB. Si la communauté n'était pas épinglée, l'opération réussit avec `unpinned: false` tout en effectuant un nettoyage défensif d'éventuelles résidus L1.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route d'administration hautement sécurisée (JWT valide + signature HMAC valide + accès réseau depuis le sous-réseau VPN WireGuard configuré par `WIREGUARD_SUBNET`).
// @Description  - Required permissions or roles: Double vérification stricte du grade Administrateur (`UserGradeAdmin = 4`) : au niveau du middleware `AdminGuard`, puis en cascade L1 → L2 → L3 dans `security_service.LeftAdmin` (qui bloque également les comptes bannis ou désactivés).
// @Description  - Relevant middleware: `MaxBodySize`, `RateLimiter`, `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `POST` (aucun paramètre métier dans l'URL).
// @Description  - Required fields (`community_recommendation_models.UnpinCommunityInput`) :
// @Description    - `conversation_id` (`int64`, `binding:"required"`) : identifiant strictement positif (`> 0`) de la communauté publique à désépingler.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Identification et binding JSON (Handler) :**
// @Description     - Extraction de `adminID` via `pkg.GetUserIDFromContext(c)`. Rejet `401` (`CodeUserIsNotIdentified`) si absent ou `<= 0`.
// @Description     - Désérialisation et vérification de présence de `conversation_id` via `c.ShouldBindJSON(&input)`.
// @Description  2. **Contrôle Zero-Trust administrateur et validation métier (`community_recommendation_service.UnpinCommunity`) :**
// @Description     - Appel de `security_service.LeftAdmin(ctx, adminID)` (cascade L1 → L2 → L3).
// @Description     - Appel de `input.Validate()` vérifiant `conversation_id > 0`.
// @Description  3. **Désépinglage atomique idempotent en PostgreSQL L3 (`postgres.FuncUnsetCommunityPin` → `messaging.func_unset_community_pin`) :**
// @Description     - Verrouillage ligne (`SELECT ... FOR UPDATE`) de la conversation ciblée ; vérification de son existence (`NOT_FOUND`) et de son type communauté publique (`type = 3`, sinon `NOT_PUBLIC_COMMUNITY`). Note : une communauté publique archivée peut tout de même être nettoyée par un administrateur.
// @Description     - Si `pin_slot IS NOT NULL`, passage atomique à `pin_slot = NULL` et mise à jour de `updated_at` ; sinon retour `OK` avec `old_pin_slot = NULL` (idempotence).
// @Description  4. **Synchronisation L1 Redis et convergence L2 MongoDB :**
// @Description     - Si un pin existait (`unpinned = true`) : suppression du slot dans `community:reco:pins:{lang}` (`DeleteCommunityPinInCache`), mise à jour L1 Object Cache et `ConvMeta` (`PinSlot = 0`), et mise en file Write-Behind `TargetMongo` (`EntityConversation`, `ActionUpdate`).
// @Description     - Si aucun pin n'existait en L3 (`unpinned = false`) et `lang != ""` : nettoyage défensif `RemoveCommunityPinByConversationID` dans `community:reco:pins:{lang}` pour résorber toute divergence éventuelle L1/L3.
// @Description  5. **Journalisation d'audit et réponse :**
// @Description     - Log structuré `Info` (`admin_id`, `conversation_id`, `lang`, `unpinned`) et réponse HTTP `200 OK`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: JSON `community_recommendation_models.UnpinCommunityOutput` :
// @Description    - `conversation_id` (`int64`) : identifiant de la communauté traitée.
// @Description    - `unpinned` (`bool`) : `true` si un épinglage actif a été retiré, `false` si la communauté n'était déjà plus épinglée (appel idempotent).
// @Description  - Persistence guarantees: Persistance synchrone L3 PostgreSQL, suppression immédiate dans le HASH L1 `community:reco:pins:{lang}` (la communauté retrouve instantanément sa position organique dans `POST /communities/recommendations`), convergence L2 MongoDB via Write-Behind.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] JSON malformé ou `conversation_id` manquant:**
// @Description    - Trigger: Corps JSON invalide ou champ `conversation_id` omis/0.
// @Description    - Execution stage: Binding GIN (`c.ShouldBindJSON`) dans le handler.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Identifiant de conversation négatif ou nul:**
// @Description    - Trigger: `conversation_id <= 0` (ex: `-1`).
// @Description    - Execution stage: Validation métier (`UnpinCommunityInput.Validate`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"L'identifiant de conversation doit être strictement positif."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Conversation cible non publique:**
// @Description    - Trigger: La conversation existe mais n'est pas de type communauté publique (`type != 3`, ex: DM `type = 1` ou groupe privé `type = 2`).
// @Description    - Execution stage: Fonction SQL `messaging.func_unset_community_pin` (statut `NOT_PUBLIC_COMMUNITY`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Seule une communauté publique peut être désépinglée."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[numan_error.CodeUnauthorized] Jeton JWT ou signature HMAC invalide:**
// @Description    - Trigger: En-tête `Authorization` absent/expiré/invalide, signature HMAC invalide, ou identifiant non authentifié dans `AdminGuard`/`LeftAdmin`.
// @Description    - Execution stage: Middlewares `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`, ou `security_service.LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (`"UNAUTHORIZED"`).
// @Description
// @Description  - **[numan_error.CodeUserIsNotIdentified] Administrateur non identifié dans le contexte:**
// @Description    - Trigger: `userID` absent ou `<= 0` dans le contexte Gin.
// @Description    - Execution stage: Extraction d'identité dans le handler (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Administrateur non identifié."`).
// @Description    - Error code: `numan_error.CodeUserIsNotIdentified` (`"USER_NOT_IDENTIFIED"`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[numan_error.CodeForbidden] Privilèges administrateur insuffisants ou compte suspendu:**
// @Description    - Trigger: L'appelant ne possède pas le grade Administrateur (`UserGradeAdmin = 4`), ou son compte administrateur est banni/désactivé.
// @Description    - Execution stage: Middleware `AdminGuard` puis vérification Zero-Trust `security_service.LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (`"FORBIDDEN_ACCESS"`).
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[numan_error.CodeNotFound] Accès hors VPN WireGuard (route masquée):**
// @Description    - Trigger: L'IP cliente n'appartient pas au CIDR `WIREGUARD_SUBNET` (défaut `10.8.0.0/24`). `AdminGuard` renvoie volontairement un `404` pour dissimuler l'existence des routes `/admin`.
// @Description    - Execution stage: Middleware `AdminGuard` (contrôle réseau initial).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound` (`"RESOURCE_NOT_FOUND"`).
// @Description
// @Description  - **[numan_error.CodeNotFound] Communauté ou compte administrateur introuvable:**
// @Description    - Trigger: Aucune ligne ne correspond à `conversation_id` dans `messaging.conversations` (statut SQL `NOT_FOUND`), ou le compte administrateur appelant n'existe pas en L1/L2/L3 (`LeftAdmin`).
// @Description    - Execution stage: `security_service.LeftAdmin` ou `messaging.func_unset_community_pin`.
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Communauté introuvable."` ou `"Compte utilisateur introuvable."`).
// @Description    - Error code: `numan_error.CodeNotFound` (`"RESOURCE_NOT_FOUND"`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeDatabaseError] Erreur PostgreSQL L3:**
// @Description    - Trigger: Connexion `PostgresDB` non initialisée ou échec d'exécution/scan de `messaging.func_unset_community_pin`.
// @Description    - Execution stage: Repository PostgreSQL (`postgres.FuncUnsetCommunityPin`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeDatabaseError` (`"DATABASE_ERROR"`).
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur interne inattendue:**
// @Description    - Trigger: Échec de lecture `GetUserLite` dans `AdminGuard`, erreur L3 dans `LeftAdmin`, ou statut SQL non reconnu.
// @Description    - Execution stage: `AdminGuard`, `security_service.LeftAdmin`, ou branche `default` de `UnpinCommunity`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (`"INTERNAL_SERVER_ERROR"`).
// @Tags         admin-communities
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                               true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                               true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                               true "Timestamp Unix de la requête"
// @Param        body          body   community_recommendation_models.UnpinCommunityInput  true "Identifiant strictement positif de la communauté publique à désépingler"
// @Success      200  {object}  community_recommendation_models.UnpinCommunityOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (JSON invalide, conversation_id <= 0 ou conversation non publique)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized (JWT/HMAC invalide ou administrateur non identifié)"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Grade Administrateur requis ou compte suspendu)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (Communauté/admin introuvable ou appel hors VPN WireGuard)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Database / Internal Server Error"
// @Router       /admin/communities/unpin [post]
func UnpinCommunityHandler(c *gin.Context) {
	ctx := c.Request.Context()

	adminID, errAuth := pkg.GetUserIDFromContext(c)
	if errAuth != nil || adminID <= 0 {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(
			numan_error.CodeUserIsNotIdentified,
			"Administrateur non identifié.",
			errAuth,
		))
		return
	}

	var input community_recommendation_models.UnpinCommunityInput
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Format de requête JSON invalide.",
			errBind,
		))
		return
	}

	output, errSvc := community_recommendation_service.UnpinCommunity(ctx, adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, output)
}
