package community_recommendation_handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/gin-gonic/gin"
)

// GetCommunityRecommendationsHandler godoc
// @Summary      Obtenir les recommandations de communautés publiques
// @Description  Retourne de manière ultra-rapide (chemin de lecture L1 Redis) la liste paginée des communautés publiques actives (`type = 3`, `state = 0`) recommandées pour une langue cible. Le classement global précalculé par le worker cron est personnalisé à la volée par affinité amis de degré 1, enrichi par injection exacte des emplacements épinglés administrativement (`1..100`), puis paginé et hydraté uniquement sur la tranche courante.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (header `Authorization: Bearer <jwt>`) ainsi qu'une signature HMAC valide (headers `X-Signature` et `X-Timestamp`).
// @Description  - Required permissions or roles: Tout utilisateur authentifié et identifié (`callerID > 0`). L'identifiant appelant est extrait exclusivement du contexte JWT (`pkg.GetUserIDFromContext`) et ne doit jamais être transmis dans le corps JSON ou l'URL.
// @Description  - Relevant middleware: `MaxBodySize`, `RateLimiter`, `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `POST` (règle API stricte : aucun paramètre métier dans le chemin ou la query string).
// @Description  - Body: JSON optionnel (`community_recommendation_models.GetCommunityRecommendationsInput`). Un corps vide (`io.EOF`) est accepté et applique toutes les valeurs par défaut.
// @Description  - Optional fields:
// @Description    - `limit` (`int`, optionnel) : nombre maximal de communautés retournées sur la page. Défaut `20` (si omis ou `0`), bornes strictes `[1, 50]` (valeurs `< 0` ou `> 50` rejetées en `400`).
// @Description    - `offset` (`int`, optionnel) : décalage 0-based dans le classement assemblé. Défaut `0`, bornes strictes `[0, 500]`.
// @Description    - `lang` (`string`, optionnel) : code langue ISO 639-1 supporté (`fr`, `en`, `es`, `it`, `pt`, `de`, `nl`, `ru`, `ar`, insensible à la casse/espaces). Si omis ou vide, la langue est résolue automatiquement depuis `UserSettings.DisplayAndContent.Language`, puis repli déterministe sur `"fr"`. Les locales composées (ex: `"fr-FR"`) et `"un"` sont rejetées.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction d'identité et binding JSON (Handler) :**
// @Description     - Extraction de `callerID` via `pkg.GetUserIDFromContext(c)`. Rejet `401` (`CodeUserIsNotIdentified`) si absent ou `<= 0`.
// @Description     - Désérialisation du JSON via `c.ShouldBindJSON(&input)` ; `io.EOF` est toléré pour permettre un appel sans body, tout autre JSON malformé est rejeté en `400` (`CodeInvalidPayload`).
// @Description  2. **Validation métier et résolution linguistique (`community_recommendation_service.GetCommunityRecommendations`) :**
// @Description     - Validation stricte via `input.Validate()` : bornes de `limit` (`0` = défaut `20`, sinon `1..50`), bornes de `offset` (`0..500`), et vérification de `lang` contre la whitelist centrale (`variables.NormalizeCommunityLang`).
// @Description     - Résolution de `resolvedLang` : `input.Lang` explicite → `object_cache_service.GetUserSettingsCascade(callerID)` → défaut `"fr"`.
// @Description  3. **Lecture du classement global précalculé (L1 Redis) :**
// @Description     - Lecture complète du ZSET `community:reco:ranked:{lang}` (`cache_service.GetRankedCommunitiesByLang`, ordre décroissant de `S_global`). En cas d'indisponibilité Redis, échec `500` (`CodeCacheError`).
// @Description  4. **Personnalisation par affinité amis de degré 1 (dégradation gracieuse) :**
// @Description     - Chargement des amis directs de l'utilisateur depuis `SpeedRelationsIndex` (`in:2` et `out:2`) capé à `CommunityRecoFriendEvalCap` (200) en 1 pipeline Redis (`GetDirectFriendIDsCapped`).
// @Description     - Agrégation en 1 pipeline Redis (`SMEMBERS`) des appartenances publiques de ces amis via `user:public_comms:{friend_id}` (`GetFriendsPublicCommunities`).
// @Description     - Application de la formule pure `ApplyFriendBoost` : `S_final = S_global * (1 + gamma * ln(1 + friends_count))` (défaut `gamma = 0.25`) avec départage déterministe (`S_final` desc, puis `S_global` desc, puis `conversation_id` asc). En cas d'erreur Redis sur les amis, la requête continue sans boost (dégradation gracieuse).
// @Description  5. **Injection exacte des épinglages administratifs (`InjectPinnedSlots`) :**
// @Description     - Lecture du HASH `community:reco:pins:{lang}` (`GetCommunityPinsByLang`, dégradation gracieuse si erreur).
// @Description     - Si une communauté fraîchement épinglée n'est pas encore dans le ZSET, hydratation ciblée via `loadConvMetasWithFallback` (filtrée `type = 3`, `state = 0`, même langue).
// @Description     - Placement exact aux indices 1-based demandés (les slots hors-bornes sont ajoutés en fin de liste par ordre de slot croissant), sans aucun doublon.
// @Description  6. **Pagination post-assemblage et hydratation batch de la page courante :**
// @Description     - Découpage `[offset : offset+limit]` après assemblage complet (`PaginateCandidates`) pour garantir l'absence de doublons aux frontières de page et calculer `total` et `has_more`.
// @Description     - Hydratation uniquement pour les `<= 50` communautés de la page : `ConvMeta` en batch `MGET` avec auto-guérison L1 Object Cache → L3 PostgreSQL (`FuncGetConversation`), et compteurs de membres en 1 pipeline `SCARD` (`ConvParticipants`).
// @Description  7. **Construction de la réponse :**
// @Description     - Résolution du mode d'interaction (`free_chat`, `announcement`, `subreddit`) via `lite_models.ResolveCommunityMode` et renvoi HTTP `200 OK`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: JSON `community_recommendation_models.GetCommunityRecommendationsOutput` :
// @Description    - `lang` (`string`) : langue effectivement utilisée pour le classement.
// @Description    - `total` (`int`) : nombre total de communautés éligibles dans le classement assemblé avant pagination.
// @Description    - `has_more` (`bool`) : indique si d'autres pages existent au-delà de `offset + len(communities)`.
// @Description    - `communities` (`[]CommunityRecommendationItem`, tableau vide `[]` jamais `null`) : `id`, `name`, `description`, `avatar_media_id`, `lang`, `mode`, `member_count`, `is_partner`, `is_pinned`, `pin_slot` (`int`, omis si `0` / non épinglée), `friends_count`.
// @Description  - Contrat volontaire : le champ `is_joined` n'est **pas** calculé ni retourné par le backend (filtrage/badge géré localement côté Flutter depuis l'inbox synchronisée).
// @Description  - Side effects: Lecture seule côté métier ; auto-guérison L1 éventuelle (`ConvMeta` / Object Cache) en cas de cache miss lors de l'hydratation de la page.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] JSON malformé ou types incompatibles:**
// @Description    - Trigger: Corps JSON syntaxiquement invalide ou champ de mauvais type (ex: `"limit": "abc"`). Note : un corps totalement vide est autorisé.
// @Description    - Execution stage: Binding JSON dans le handler (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Paramètre `limit` hors bornes:**
// @Description    - Trigger: `limit` fourni avec une valeur `< 0` (ex: `-5`) ou `> 50` (ex: `51`). La valeur `0` (ou omission) applique le défaut `20`.
// @Description    - Execution stage: Validation du domaine (`GetCommunityRecommendationsInput.Validate`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Le paramètre limit doit être compris entre 1 et 50."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Paramètre `offset` hors bornes:**
// @Description    - Trigger: `offset` fourni avec une valeur `< 0` ou `> 500`.
// @Description    - Execution stage: Validation du domaine (`GetCommunityRecommendationsInput.Validate`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Le paramètre offset doit être compris entre 0 et 500."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Langue explicite non supportée:**
// @Description    - Trigger: `lang` non vide ne correspondant pas à un code ISO 639-1 supporté (`fr`, `en`, `es`, `it`, `pt`, `de`, `nl`, `ru`, `ar`), par exemple `"jp"`, `"un"`, `"fr-FR"` ou `"> 8 caractères"`.
// @Description    - Execution stage: Validation du domaine (`GetCommunityRecommendationsInput.Validate` → `variables.NormalizeCommunityLang`).
// @Description    - Response: `numan_error.PublicErrorResponse` (`"La langue demandée n'est pas supportée."`).
// @Description    - Error code: `numan_error.CodeInvalidPayload` (`"INVALID_PAYLOAD"`).
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[numan_error.CodeUnauthorized] Jeton JWT ou signature HMAC invalide:**
// @Description    - Trigger: En-tête `Authorization` absent/expiré/révoqué, ou en-têtes `X-Signature` / `X-Timestamp` manquants, expirés ou invalides.
// @Description    - Execution stage: Middlewares `JWTMiddleware` / `HMACMiddleware` avant l'entrée dans le handler.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (`"UNAUTHORIZED"`).
// @Description
// @Description  - **[numan_error.CodeUserIsNotIdentified] Utilisateur absent du contexte HTTP:**
// @Description    - Trigger: Le contexte Gin ne contient pas de `userID` valide (`<= 0` ou absent).
// @Description    - Execution stage: Contrôle d'identité initial dans le handler (`pkg.GetUserIDFromContext`) ou garde d'entrée du service.
// @Description    - Response: `numan_error.PublicErrorResponse` (`"Utilisateur non identifié."`).
// @Description    - Error code: `numan_error.CodeUserIsNotIdentified` (`"USER_NOT_IDENTIFIED"`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeCacheError] Indisponibilité ou erreur de lecture du cache Redis L1:**
// @Description    - Trigger: Les collections Redis de recommandation communautaire ne sont pas initialisées ou la commande `ZREVRANGE ... WITHSCORES` sur `community:reco:ranked:{lang}` échoue (coupure réseau/timeout Redis).
// @Description    - Execution stage: Lecture du ZSET global via `cache_service.GetRankedCommunitiesByLang`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError` (`"CACHE_ERROR"`).
// @Tags         communities
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                                            true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                                            true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                                            true  "Timestamp Unix de la requête"
// @Param        body          body   community_recommendation_models.GetCommunityRecommendationsInput  false "Paramètres optionnels de pagination (limit 1..50, offset 0..500) et filtre de langue ISO 639-1"
// @Success      200  {object}  community_recommendation_models.GetCommunityRecommendationsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (JSON malformé, limit/offset hors bornes ou langue non supportée)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized (JWT/HMAC invalide ou utilisateur non identifié)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Cache Error (Redis L1 indisponible lors de la lecture du classement)"
// @Router       /communities/recommendations [post]
func GetCommunityRecommendationsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	callerID, errAuth := pkg.GetUserIDFromContext(c)
	if errAuth != nil || callerID <= 0 {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(
			numan_error.CodeUserIsNotIdentified,
			"Utilisateur non identifié.",
			errAuth,
		))
		return
	}

	var input community_recommendation_models.GetCommunityRecommendationsInput
	if errBind := c.ShouldBindJSON(&input); errBind != nil && !errors.Is(errBind, io.EOF) {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Format de requête JSON invalide.",
			errBind,
		))
		return
	}

	output, errSvc := community_recommendation_service.GetCommunityRecommendations(ctx, callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, output)
}
