package article_handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/article_service"
	"github.com/gin-gonic/gin"
)

// ListArticlesHandler godoc
// @Summary      Lister les articles Markdown (paginé, ultra-léger)
// @Description  Retourne la liste paginée et ultra-légère (~1-2 Ko par item) des articles triés par date de création décroissante (`created_at DESC`, puis `id DESC`) depuis l'index Redis L1 persistant (`articles:index:order` + `articles:index:data`, TTL 0, auto-réparé depuis MongoDB L2 / PostgreSQL L3 en cas de cache vide non initialisé ou de membres désynchronisés). Les utilisateurs standards reçoivent exclusivement les articles publiés (`visibility == 0`) et non expirés ; les administrateurs reçoivent également les brouillons (`visibility == 1`) et les articles expirés. N'expose jamais ni le contenu Markdown, ni `markdown_url`, ni `attachments` (seule `cover_url` est hydratée en un unique appel `MGET` batch).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`) et une signature HMAC valide (`X-Signature`, `X-Timestamp`).
// @Description  - Routes et rôles:
// @Description    - `POST /articles/list`: accessible à tout utilisateur authentifié (les administrateurs y bénéficient automatiquement de la vue étendue brouillons/expirés via `isCallerAdmin`).
// @Description    - `POST /admin/articles/list`: alias monté sur `adminGroup`, protégé en amont par `AdminGuard` (VPN WireGuard `WIREGUARD_SUBNET` + grade Administrateur `4` ou `1`).
// @Description  - Relevant middleware: `MaxBodySize` (limite JSON `2 MiB`), `RateLimiter` (50 req/10s/IP), `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware` (et `AdminGuard` sur `/admin/articles/list`).
// @Description  - URL restrictions: Aucun paramètre de query string URL toléré (`rejectQueryParameters`).
// @Description
// @Description  **Request Contract:**
// @Description  - Routes: `POST /articles/list` (utilisateur / admin) et `POST /admin/articles/list` (admin sous WireGuard).
// @Description  - Body JSON (`article_models.GetArticlesInput`, optionnel — un corps vide `io.EOF` est toléré et applique les valeurs par défaut) :
// @Description    - `limit` (int, optionnel): `0` ou omis applique le défaut `20` (`variables.DefaultArticleListLimit`) ; toute valeur `> 100` est bornée à `100` (`variables.MaxArticleListLimit`) ; toute valeur `< 0` est rejetée (`400`).
// @Description    - `offset` (int, optionnel): `>= 0` (défaut `0`) ; toute valeur `< 0` est rejetée (`400`).
// @Description    - `include_drafts` / `include_expired` (bool, optionnels dans le DTO): le filtrage brouillons/expirés est imposé côté serveur selon le rôle réel (`isCallerAdmin`) — un non-admin ne peut pas contourner la visibilité via ces champs.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Rejet des query parameters (`rejectQueryParameters`) et extraction de `callerID` (`pkg.GetUserIDFromContext`).
// @Description  2. Binding JSON optionnel de `article_models.GetArticlesInput` (les erreurs `io.EOF` dues à un body vide sont explicitement tolérées ; tout autre défaut JSON retourne `400`).
// @Description  3. Détermination du rôle administrateur via `isCallerAdmin`.
// @Description  4. Validation et normalisation de la pagination (`input.ValidateAndNormalize()`).
// @Description  5. Lecture complète de l'index Redis L1 via `object_cache_service.ListArticlesFromCache` :
// @Description     - Lecture des IDs ordonnés (`ZREVRANGE articles:index:order 0 -1`) et des payloads MsgPack (`HMGET articles:index:data`).
// @Description     - Si l'index est vide et non marqué initialisé (`articles:index:initialized`), déclenchement de `RebuildArticlesIndex` (verrou distribué Redis + mutex local, chargement L2 MongoDB puis L3 PostgreSQL).
// @Description     - Si certains IDs du ZSET manquent ou sont corrompus dans le HASH, auto-réparation batch via `healMissingArticlesByIDs` (L2 -> L3) et nettoyage des entrées orphelines/supprimées.
// @Description  6. Filtrage en mémoire : exclusion systématique des articles `visibility < 0` ; pour les non-admins, exclusion des brouillons (`visibility != 0`) et des articles expirés (`expires_at <= nowMs`).
// @Description  7. Calcul de `total`, découpage de la tranche `[offset : min(offset+limit, total)]`, et hydratation batch de `cover_url` uniquement (`BuildArticleListItemViews` en un seul `MGET` + fallback cascade).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: `article_models.GetArticlesOutput` (`articles` []`ArticleListItemView`, `total`, `limit`, `offset`), réponse signée par `HMACMiddleware`.
// @Description  - Performance guarantees: Aucune requête SQL/Mongo à chaud lorsque l'index L1 est peuplé ; latence sub-milliseconde et payload ultra-léger.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Query parameters interdits, JSON malformé ou pagination négative:**
// @Description    - Trigger: Présence de query parameters dans l'URL, JSON syntaxiquement invalide (autre qu'un corps vide `io.EOF`), `limit < 0` ou `offset < 0`.
// @Description    - Execution stage: `rejectQueryParameters`, `pkg.GetUserIDFromContext` (type `userID` invalide), `ShouldBindJSON`, ou `GetArticlesInput.ValidateAndNormalize`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED / USER_NOT_IDENTIFIED / MISSING_TOKEN / INVALID_TOKEN / EXPIRED_TOKEN / MISSING_HEADERS] Authentification manquante ou invalide:**
// @Description    - Trigger: Token JWT absent/expiré/invalide, headers `X-Signature` / `X-Timestamp` manquants ou invalides, ou `userID` absent du contexte.
// @Description    - Execution stage: `JWTMiddleware`, `HMACMiddleware`, `AdminGuard` (sur `/admin/articles/list`) ou `pkg.GetUserIDFromContext`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` / `numan_error.CodeUserIsNotIdentified` (ou code middleware exact).
// @Description
// @Description  ⛔ **403 Forbidden (uniquement sur l'alias `/admin/articles/list`):**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Privilèges d'administrateur requis:**
// @Description    - Trigger: Requête envoyée sur `/admin/articles/list` (depuis le VPN WireGuard) par un utilisateur non administrateur. (La route `/articles/list` ne renvoie jamais `403`).
// @Description    - Execution stage: `AdminGuard` sur `adminGroup`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟤 **404 Not Found (uniquement sur l'alias `/admin/articles/list`):**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] IP cliente hors VPN WireGuard:**
// @Description    - Trigger: Requête envoyée sur `/admin/articles/list` depuis une IP extérieure à `WIREGUARD_SUBNET`. (La route `/articles/list` retourne un tableau vide avec `200 OK` lorsqu'il n'y a aucun article).
// @Description    - Execution stage: `AdminGuard` sur `adminGroup`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  📦 **413 Payload Too Large:**
// @Description
// @Description  - **[PAYLOAD_TOO_LARGE] Corps JSON > 2 MiB:**
// @Description    - Trigger: Requête JSON dont la taille dépasse `2 MiB`.
// @Description    - Execution stage: Middleware global `MaxBodySize`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodePayloadTooLarge` (`"PAYLOAD_TOO_LARGE"`).
// @Description
// @Description  🚦 **429 Too Many Requests:**
// @Description
// @Description  - **[TOO_MANY_REQUESTS] Dépassement du quota anti-flood:**
// @Description    - Trigger: Plus de 50 requêtes sur 10 secondes depuis la même IP cliente.
// @Description    - Execution stage: Middleware global `RateLimiter`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeTooManyRequests` (`"TOO_MANY_REQUESTS"`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[CACHE_ERROR] Erreur Redis L1 lors de la lecture ou reconstruction d'index:**
// @Description    - Trigger: Client Redis nil, erreur `ZRevRange` sur `articles:index:order`, erreur `HMGet` sur `articles:index:data`, ou échec de persistance dans `RebuildArticlesIndex`.
// @Description    - Execution stage: `object_cache_service.ListArticlesFromCache` ou `RebuildArticlesIndex`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Échec technique interne:**
// @Description    - Trigger: Échec `GetUserLite` dans `AdminGuard` sur `/admin/articles/list` ou échec de reconstruction de l'index depuis L2/L3.
// @Description    - Execution stage: `AdminGuard` ou `RebuildArticlesIndex`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         articles
// @Accept       json
// @Produce      json
// @Param        Authorization header string                          true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                          true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                          true  "Timestamp Unix de la requête"
// @Param        input         body   article_models.GetArticlesInput false "Paramètres de pagination (limit, offset) — corps vide autorisé"
// @Success      200  {object}  article_models.GetArticlesOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (on /admin/articles/list only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (on /admin/articles/list outside WireGuard VPN)"
// @Failure      413  {object}  numan_error.PublicErrorResponse "Payload Too Large (> 2 MiB)"
// @Failure      429  {object}  numan_error.PublicErrorResponse "Too Many Requests"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Cache or Internal Server Error"
// @Router       /articles/list [post]
func ListArticlesHandler(c *gin.Context) {
	if errQuery := rejectQueryParameters(c); errQuery != nil {
		numan_error.RespondWithError(c, errQuery)
		return
	}

	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input article_models.GetArticlesInput
	if errBind := c.ShouldBindJSON(&input); errBind != nil && !errors.Is(errBind, io.EOF) {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", errBind))
		return
	}

	isAdmin := isCallerAdmin(c, callerID)
	out, errSvc := article_service.ListArticlesService(c.Request.Context(), callerID, isAdmin, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
