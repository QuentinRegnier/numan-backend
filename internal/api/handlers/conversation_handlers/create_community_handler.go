package conversation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
	"github.com/gin-gonic/gin"
)

// CreateCommunityHandler godoc
// @Summary      Créer une communauté publique
// @Description  Crée une nouvelle communauté publique (`type = 3`, `state = 0`) avec sa langue obligatoire (ISO 639-1), ses paramètres d'interaction et son lien externe éventuel. Seuls les utilisateurs disposant d'un grade suffisant (`CommunityMinCreationGrade`) peuvent créer une communauté (dans la limite du quota collaborateur), avec possibilité de délégation de propriété à un tiers pour les Modérateurs/Administrateurs. La création initialise immédiatement les caches L1 (Object Cache, Speed Cache, recherche communautaire, `user:public_comms:{owner_id}`), insère un score initial dans le ZSET de recommandation `community:reco:ranked:{lang}`, et persiste en arrière-plan vers MongoDB/PostgreSQL via Write-Behind.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`) et une signature HMAC valide (`X-Signature`, `X-Timestamp`).
// @Description  - Required permissions or roles: Grade utilisateur `>= CommunityMinCreationGrade` (`2`, Collaborateur). Les Collaborateurs (Grade 2) sont plafonnés à `MaxPublicCommunitiesColl` communauté(s) détenue(s). La délégation (`owner_id != callerID`) exige un grade `>= UserGradeModerator` (`3`).
// @Description  - Relevant middleware: `MaxBodySize`, `RateLimiter`, `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `POST` (aucun paramètre métier dans l'URL).
// @Description  - Required fields (`conversation_models.CreateCommunityInput`) :
// @Description    - `title` (`string`, `binding:"required,min=3,max=100"`) : nom de la communauté (entre 3 et 100 runes après `TrimSpace`).
// @Description    - `lang` (`string`, `binding:"required"`) : code langue ISO 639-1 obligatoire parmi la whitelist supportée (`fr`, `en`, `es`, `it`, `pt`, `de`, `nl`, `ru`, `ar`, normalisé en minuscules).
// @Description  - Optional fields:
// @Description    - `settings` (`conversation_models.ConversationSettings`) : règles d'écriture/adhésion (par défaut `DefaultConversationSettings(3)`, dont `write_permission = 2` soit mode `subreddit`).
// @Description    - `external_link` (`models.ExternalLinks`) : lien externe validé (URL + titre).
// @Description    - `owner_id` (`int64`, `>= 0`) : identifiant du propriétaire cible en cas de délégation par un Modérateur/Administrateur.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction d'identité et binding JSON (Handler) :**
// @Description     - Extraction de `callerID` via `pkg.GetUserIDFromContext(c)`. Rejet `401` (`CodeUserIsNotIdentified`) si absent.
// @Description     - Binding GIN via `c.ShouldBindJSON(&input)` (`title` et `lang` obligatoires).
// @Description  2. **Validation métier et normalisation linguistique (`conversation_service.CreateCommunity`) :**
// @Description     - Contrôle via `input.Validate()` (longueur `title`, `lang` supporté via `variables.NormalizeCommunityLang`, `owner_id >= 0`, `settings.Validate()`, `external_link.Validate()`).
// @Description  3. **Contrôle du grade, du quota et de la délégation :**
// @Description     - Chargement du profil appelant via `cache_service.GetUserLite(callerID)`. Rejet `404` si introuvable.
// @Description     - Rejet `403` si `grade < CommunityMinCreationGrade` ou si un Collaborateur a atteint `MaxPublicCommunitiesColl`.
// @Description     - Si `owner_id != 0 && owner_id != callerID` : rejet `403` si `grade < UserGradeModerator`, rejet `400` si l'utilisateur cible n'existe pas.
// @Description  4. **Initialisation des payloads et validation domaine :**
// @Description     - Génération des IDs Snowflake (`ConversationPayload` Type 3 avec `Lang` normalisé, `MemberPayload` Owner, et éventuellement `MemberPayload` Normal pour l'admin délégateur) puis `Validate()` sur chaque entité.
// @Description  5. **Indexation L1, Speed Cache et Recommandation de Communautés :**
// @Description     - Écriture L1 Object Cache (`Conversation`, `Member`), réhydratation Speed Cache (`RehydrateConversationItemInSpeedCache`) et index de recherche (`StoreCommunityLiteInSpeedCache`).
// @Description     - Enregistrement dans `user:public_comms:{owner_id}` (et `caller_id` en cas de délégation) via `AddUserPublicCommunity`.
// @Description     - Calcul du score initial (`community_recommendation_service.ComputeInitialCreationScore`) et insertion dans le ZSET `community:reco:ranked:{lang}` (`UpsertRankedCommunityScore`).
// @Description  6. **Persistance Asynchrone Write-Behind et activité Inbox :**
// @Description     - Enqueue dans `redis.EnqueueDB` (`ActionCreate`, `TargetAll`) pour `EntityConversation` et chaque `EntityMembers`, marquage `TouchInboxActivity(callerID)` et réponse HTTP `201 Created`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `201 Created`
// @Description  - Response body: JSON `conversation_models.CreateCommunityOutput` (`conversation_id`, `conversation_update_at`).
// @Description  - Persistence guarantees: Cache L1 immédiat (Object Cache, `ConvMeta`, `user:public_comms`, `community:reco:ranked:{lang}`), durabilité L2 MongoDB + L3 PostgreSQL via Write-Behind.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] JSON malformé ou champs obligatoires absents:**
// @Description    - Trigger: Corps JSON invalide, `title` manquant (< 3 ou > 100 caractères), ou `lang` manquant/vide.
// @Description    - Execution stage: Binding GIN (`c.ShouldBindJSON`) ou validation métier (`CreateCommunityInput.Validate`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Langue non supportée:**
// @Description    - Trigger: `lang` ne fait pas partie des codes ISO 639-1 supportés (`fr`, `en`, `es`, `it`, `pt`, `de`, `nl`, `ru`, `ar`), par exemple `"jp"`, `"un"` ou `"fr-FR"`.
// @Description    - Execution stage: Validation métier (`CreateCommunityInput.Validate` → `variables.NormalizeCommunityLang`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"La langue de la communauté est obligatoire et doit faire partie des langues supportées."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Propriétaire délégué inexistant ou paramètres invalides:**
// @Description    - Trigger: `owner_id < 0`, utilisateur `owner_id` introuvable en cache lors d'une délégation, `settings` invalides ou `external_link` invalide.
// @Description    - Execution stage: Validation métier (`CreateCommunityInput.Validate` / résolution `owner_id` dans `CreateCommunity`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
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
// @Description  - **[numan_error.CodeForbidden] Grade insuffisant, quota atteint ou délégation non autorisée:**
// @Description    - Trigger: Grade appelant `< CommunityMinCreationGrade` (2), Collaborateur possédant déjà `>= MaxPublicCommunitiesColl` communauté(s) publique(s), ou tentative de définir un `owner_id` tiers avec un grade `< UserGradeModerator` (3).
// @Description    - Execution stage: Contrôles de rang, quota et délégation dans `conversation_service.CreateCommunity`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (`"FORBIDDEN_ACCESS"`).
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[numan_error.CodeNotFound] Profil demandeur introuvable:**
// @Description    - Trigger: Le profil L1 `GetUserLite(callerID)` du demandeur n'existe plus ou est introuvable.
// @Description    - Execution stage: Étape 1 de `conversation_service.CreateCommunity`.
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Profil demandeur introuvable."`).
// @Description    - Error code: `numan_error.CodeNotFound` (`"RESOURCE_NOT_FOUND"`).
// @Tags         conversations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   conversation_models.CreateCommunityInput true "Payload de création (title et lang ISO 639-1 requis)"
// @Success      201  {object}  conversation_models.CreateCommunityOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (JSON invalide, title/lang/settings/external_link invalides ou owner_id inexistant)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized (JWT/HMAC invalide ou utilisateur non identifié)"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Grade insuffisant, quota atteint ou délégation refusée)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (Profil demandeur introuvable)"
// @Router       /conversation/community/set [post]
func CreateCommunityHandler(c *gin.Context) {
	// 1. Extraction sécurisée de l'ID utilisateur
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	// 2. Parsing du Body
	var input conversation_models.CreateCommunityInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le format des données est invalide.", err))
		return
	}

	// 3. Appel du service métier pur
	output, err := conversation_service.CreateCommunity(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	// 4. Réponse
	c.JSON(http.StatusCreated, output)
}
