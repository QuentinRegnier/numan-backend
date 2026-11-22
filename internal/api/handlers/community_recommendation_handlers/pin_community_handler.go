package community_recommendation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/gin-gonic/gin"
)

// PinCommunityHandler godoc
// @Summary      Épingler une communauté publique sur un slot fixe (Admin)
// @Description  Permet exclusivement à un Administrateur (Grade 4) d'épingler atomiquement une communauté publique active (`type = 3`, `state = 0`) à une position 1-based exacte (`1..100`) dans les recommandations de sa langue. La mutation est exécutée en transaction verrouillée dans PostgreSQL L3 (avec contrôle de collision et option `force_replace`), puis propagée immédiatement dans le HASH Redis `community:reco:pins:{lang}`, les caches L1 (`Object Cache`, `ConvMeta`) et la file Write-Behind L2 MongoDB.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route d'administration hautement sécurisée (JWT valide + signature HMAC valide + accès réseau depuis le sous-réseau VPN WireGuard configuré par `WIREGUARD_SUBNET`).
// @Description  - Required permissions or roles: Double vérification stricte du grade Administrateur (`UserGradeAdmin = 4`) : au niveau du middleware `AdminGuard`, puis en cascade L1 → L2 → L3 dans `security_service.LeftAdmin` (qui bloque également les comptes bannis ou désactivés).
// @Description  - Relevant middleware: `MaxBodySize`, `RateLimiter`, `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `POST` (aucun paramètre métier dans l'URL).
// @Description  - Required fields (`community_recommendation_models.PinCommunityInput`) :
// @Description    - `conversation_id` (`int64`, `binding:"required"`) : identifiant strictement positif (`> 0`) de la communauté publique active à épingler.
// @Description    - `slot` (`int`, `binding:"required"`) : position 1-based d'épinglage, strictement comprise entre `1` et `100`.
// @Description  - Optional fields:
// @Description    - `force_replace` (`bool`, défaut `false`) : si `true` et que le `slot` est déjà occupé par une autre communauté active de la même langue, désépingle atomiquement l'ancienne communauté (`pin_slot = NULL`) et retourne son identifiant dans `displaced_conversation_id`. Si `false`, renvoie `409 Conflict`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Identification et binding JSON (Handler) :**
// @Description     - Extraction de `adminID` via `pkg.GetUserIDFromContext(c)`. Rejet `401` (`CodeUserIsNotIdentified`) si absent ou `<= 0`.
// @Description     - Validation syntaxique et présence des champs requis (`conversation_id`, `slot`) via `c.ShouldBindJSON(&input)`.
// @Description  2. **Contrôle Zero-Trust administrateur et validation métier (`community_recommendation_service.PinCommunity`) :**
// @Description     - Appel de `security_service.LeftAdmin(ctx, adminID)` (L1 `UsersLite` → L2 MongoDB → L3 PostgreSQL).
// @Description     - Appel de `input.Validate()` vérifiant `conversation_id > 0` et `1 <= slot <= 100`.
// @Description  3. **Mutation atomique PostgreSQL L3 (`postgres.FuncSetCommunityPin` → `messaging.func_set_community_pin`) :**
// @Description     - Verrouillage ligne (`SELECT ... FOR UPDATE`) de la communauté cible ; contrôle de son existence (`NOT_FOUND`), de son statut communauté publique active (`type = 3 AND state = 0`, sinon `NOT_ACTIVE_PUBLIC_COMMUNITY`) et de la présence d'une langue valide (`MISSING_LANG`).
// @Description     - Si la communauté occupe déjà ce même slot, retour `OK` idempotent sans écriture inutile.
// @Description     - Recherche verrouillée (`FOR UPDATE`) d'un occupant concurrent sur `(lang, slot)`. En cas de collision : retour `CONFLICT` (409) si `force_replace = false`, sinon désépinglage atomique de l'occupant (`displaced_conv_id`) puis mise à jour `pin_slot = slot` sur la cible.
// @Description  4. **Synchronisation immédiate L1 Redis et convergence L2 MongoDB :**
// @Description     - Si la communauté cible occupait un ancien slot différent (`old_pin_slot != slot`), suppression de cet ancien slot dans `community:reco:pins:{lang}` (`DeleteCommunityPinInCache`).
// @Description     - Écriture du nouveau slot dans `community:reco:pins:{lang}` (`SetCommunityPinInCache`, effet immédiat sur `POST /communities/recommendations`).
// @Description     - Synchronisation L1 Object Cache, L1 `ConvMeta` et mise en file Write-Behind `TargetMongo` (`EntityConversation`, `ActionUpdate`) pour la communauté ciblée ainsi que pour la communauté déplacée éventuelle (`PinSlot = 0`).
// @Description  5. **Journalisation d'audit et réponse :**
// @Description     - Log structuré `Info` (`admin_id`, `conversation_id`, `lang`, `slot`, `force_replace`, `displaced_conversation_id`) et réponse HTTP `200 OK`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: JSON `community_recommendation_models.PinCommunityOutput` :
// @Description    - `conversation_id` (`int64`) : identifiant de la communauté épinglée.
// @Description    - `lang` (`string`) : langue de la communauté dans laquelle le slot est réservé.
// @Description    - `pin_slot` (`int`) : slot attribué (`1..100`).
// @Description    - `displaced_conversation_id` (`int64`, omis si `0` / aucun remplacement) : identifiant de la communauté précédemment épinglée sur ce slot lorsque `force_replace = true`.
// @Description  - Persistence guarantees: Persistance synchrone et atomique en L3 PostgreSQL (source de vérité), visibilité instantanée en L1 Redis (`community:reco:pins:{lang}`, `ConvMeta`, Object Cache), convergence L2 MongoDB via Write-Behind.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] JSON malformé ou champs requis absents:**
// @Description    - Trigger: Corps JSON invalide, `conversation_id` omis/0, ou `slot` omis/0.
// @Description    - Execution stage: Binding GIN (`c.ShouldBindJSON`) dans le handler.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Identifiant ou slot hors bornes:**
// @Description    - Trigger: `conversation_id <= 0` ou `slot < 1` ou `slot > 100`.
// @Description    - Execution stage: Validation métier (`PinCommunityInput.Validate`) ou garde SQL (`INVALID_SLOT`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Cible non éligible (pas une communauté publique active):**
// @Description    - Trigger: La conversation existe mais est un DM (`type = 1`), un groupe privé (`type = 2`), ou une communauté archivée/inactive (`state != 0`).
// @Description    - Execution stage: Fonction SQL `messaging.func_set_community_pin` (statut `NOT_ACTIVE_PUBLIC_COMMUNITY`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Seule une communauté publique active peut être épinglée."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Communauté sans langue définie:**
// @Description    - Trigger: La colonne `lang` de la communauté en base L3 est `NULL` ou vide.
// @Description    - Execution stage: Fonction SQL `messaging.func_set_community_pin` (statut `MISSING_LANG`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"La communauté doit posséder une langue valide avant d'être épinglée."`).
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
// @Description    - Trigger: L'appelant possède un grade inférieur à Administrateur (`UserGradeAdmin = 4`), ou son compte administrateur est banni/désactivé.
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
// @Description    - Trigger: Aucune ligne ne correspond à `conversation_id` dans `messaging.conversations` (statut SQL `NOT_FOUND`), ou le compte administrateur appelant n'existe dans aucune couche L1/L2/L3 (`LeftAdmin`).
// @Description    - Execution stage: `security_service.LeftAdmin` ou `messaging.func_set_community_pin`.
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Communauté introuvable."` ou `"Compte utilisateur introuvable."`).
// @Description    - Error code: `numan_error.CodeNotFound` (`"RESOURCE_NOT_FOUND"`).
// @Description
// @Description  🟡 **409 Conflict:**
// @Description
// @Description  - **[numan_error.CodeConflict] Slot d'épinglage déjà occupé dans cette langue:**
// @Description    - Trigger: Une autre communauté publique active de la même langue occupe déjà `slot` et `force_replace` vaut `false` (ou est omis).
// @Description    - Execution stage: Contrôle de collision verrouillé dans `messaging.func_set_community_pin` (statut `CONFLICT`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Ce slot d'épinglage est déjà occupé pour cette langue. Utilisez force_replace=true pour remplacer l'épinglage existant."`).
// @Description    - Error code: `numan_error.CodeConflict` (`"RESOURCE_CONFLICT"`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeDatabaseError] Erreur PostgreSQL L3:**
// @Description    - Trigger: Connexion `PostgresDB` non initialisée ou échec d'exécution/scan de `messaging.func_set_community_pin`.
// @Description    - Execution stage: Repository PostgreSQL (`postgres.FuncSetCommunityPin`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeDatabaseError` (`"DATABASE_ERROR"`).
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur interne inattendue:**
// @Description    - Trigger: Échec de lecture `GetUserLite` dans `AdminGuard`, erreur L3 dans `LeftAdmin`, ou statut SQL non reconnu.
// @Description    - Execution stage: `AdminGuard`, `security_service.LeftAdmin`, ou branche `default` de `PinCommunity`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (`"INTERNAL_SERVER_ERROR"`).
// @Tags         admin-communities
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                             true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                             true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                             true "Timestamp Unix de la requête"
// @Param        body          body   community_recommendation_models.PinCommunityInput  true "Paramètres d'épinglage (conversation_id > 0, slot 1..100, force_replace optionnel)"
// @Success      200  {object}  community_recommendation_models.PinCommunityOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (JSON invalide, slot hors 1..100, cible non publique/inactive ou sans langue)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized (JWT/HMAC invalide ou administrateur non identifié)"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Grade Administrateur requis ou compte suspendu)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (Communauté/admin introuvable ou appel hors VPN WireGuard)"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Slot déjà occupé pour cette langue avec force_replace=false)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Database / Internal Server Error"
// @Router       /admin/communities/pin [post]
func PinCommunityHandler(c *gin.Context) {
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

	var input community_recommendation_models.PinCommunityInput
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Format de requête JSON invalide.",
			errBind,
		))
		return
	}

	output, errSvc := community_recommendation_service.PinCommunity(ctx, adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, output)
}
