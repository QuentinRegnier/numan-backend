package article_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/article_service"
	"github.com/gin-gonic/gin"
)

// CreateArticleHandler godoc
// @Summary      Créer un article Markdown (Admin)
// @Description  Permet exclusivement à un Administrateur de créer un article Markdown en rattachant un document Markdown public obligatoire (`markdown_media_id`), une image de couverture optionnelle (`cover_media_id`) et des pièces jointes visuelles optionnelles (`attachment_media_ids`). Valide la sémantique et le préfixe public `articles/` (autorisant le partage cross-admin tout en bloquant tout média privé `users/...`), active les médias en `MediaVisibilityPublic` (`1`), indexe l'article dans Redis L1 (`TTL 0`, non-évictable) et planifie la persistance Write-Behind vers MongoDB L2 et PostgreSQL L3.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`), une signature HMAC valide (`X-Signature`, `X-Timestamp`) et un accès réseau VPN WireGuard (`WIREGUARD_SUBNET`, défaut `10.8.0.0/24`).
// @Description  - Required permissions or roles: Grade Administrateur strictement requis (`variables.UserGradeAdmin = 4` ou fallback `variables.MemberRoleAdmin = 1`), appliqué en amont par le middleware `AdminGuard` sur le groupe `/admin` dans `routes.go`.
// @Description  - Relevant middleware: `MaxBodySize` (limite JSON `2 MiB`), `RateLimiter` (50 req/10s/IP), `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`.
// @Description  - URL restrictions: Aucun paramètre de query string URL toléré (`rejectQueryParameters`).
// @Description
// @Description  **Request Contract:**
// @Description  - Routes: `POST /admin/articles/create` (et alias `POST /admin/articles/set`).
// @Description  - Body JSON (`article_models.CreateArticleInput`):
// @Description    - `title` (string, **requis**): nettoyé via `TrimSpace`, longueur comprise entre `1` et `200` runes (`variables.MaxArticleTitleLength`).
// @Description    - `markdown_media_id` (int64, **requis**): strictement positif (`> 0`), doit référencer un média existant sous préfixe `articles/` d'extension `.md` (statut `Pending` `0` ou `Public` `1`).
// @Description    - `summary` (string, optionnel): nettoyé via `TrimSpace`, max `500` runes (`variables.MaxArticleSummaryLength`).
// @Description    - `cover_media_id` (int64, optionnel): `>= 0` (`0` = aucune couverture ; si `> 0`, doit référencer un média sous `articles/` d'extension `.avif`, `.gif` ou `.webp`).
// @Description    - `attachment_media_ids` ([]int64, optionnel): chaque ID doit être `> 0`, dédupliqué côté serveur en préservant l'ordre, max `50` pièces jointes (`variables.MaxArticleAttachments`), extension `.avif`/`.gif`/`.webp` sous `articles/`.
// @Description    - `partner_url` (string, optionnel): max `512` caractères, sans espace ni `\t`/`\r`/`\n`/`\x00`, schéma strictement `http` ou `https` avec nom d'hôte non vide.
// @Description    - `visibility` (*int, optionnel): `0` (`ArticleVisibilityPublished`, défaut si omis) ou `1` (`ArticleVisibilityDraft`). La valeur `-1` (soft-deleted) est interdite à la création.
// @Description    - `expires_at` (*int64, optionnel): timestamp Unix en millisecondes ; `<= 0` est normalisé à `nil` (pas d'expiration) ; si `> 0`, doit être strictement supérieur à l'heure courante (`expires_at > nowMs`).
// @Description    - `id` (int64, optionnel): `>= 0` (`0` déclenche la génération automatique d'un ID Snowflake via `pkg.GenerateID()`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Rejet des query parameters (`rejectQueryParameters`) et extraction de `adminID` (`pkg.GetUserIDFromContext`) après contrôle `AdminGuard`.
// @Description  2. Binding JSON dans `article_models.CreateArticleInput` (`c.ShouldBindJSON`).
// @Description  3. Validation et normalisation des champs métier (`input.ValidateAndNormalize(nowMs)`).
// @Description  4. Génération de l'identifiant Snowflake si `input.ID <= 0`.
// @Description  5. Validation tout-ou-rien et activation cross-admin des médias (`media_service.ValidateAndActivateAdminArticleMedia`) : vérification en cascade (L1 -> L2 -> L3) de chaque média, contrôle du préfixe `articles/`, rejet des médias `Rejected` (`3`) ou non activables, vérification d'extension sémantique (`.md` vs visuel), puis passage en `MediaVisibilityPublic` (`1`) sans quarantaine avec écriture Redis L1 et `EnqueueDB` (`EntityMedia`, `ActionUpdate`, `TargetAll`).
// @Description  6. Construction et re-validation de `article_models.ArticlePayload` (`CreatedAt = UpdatedAt = nowMs`).
// @Description  7. Écriture synchrone dans l'index Redis L1 persistant (`object_cache_service.SetArticleInCache` : HASH MsgPack `articles:index:data`, ZSET `articles:index:order` scoré sur `CreatedAt`, marqueur `articles:index:initialized`, TTL = `0`).
// @Description  8. Planification Write-Behind (`redis.EnqueueDB` avec `EntityArticle`, `ActionCreate`, `TargetAll`). En cas d'échec de mise en file, rollback immédiat de l'entrée Redis L1 (`DeleteArticleFromCache`).
// @Description  9. Construction et retour de `article_models.CreateArticleOutput` via `BuildArticleDetailView` (résolution batch des URLs publiques sans jamais télécharger le contenu Markdown côté serveur).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: `article_models.CreateArticleOutput` encapsulant `ArticleDetailView`, réponse signée par `HMACMiddleware`.
// @Description  - Persistence guarantees: Article immédiatement lisible dans l'index Redis L1 persistant (TTL 0), médias activés en `Public (1)` et persistance asynchrone garantie vers MongoDB L2 et PostgreSQL L3.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Requête malformée, champs invalides ou médias non conformes:**
// @Description    - Trigger: Présence de query parameters dans l'URL, JSON syntaxiquement invalide, `title` vide ou `> 200` runes, `summary > 500` runes, `id < 0`, `cover_media_id < 0`, `markdown_media_id <= 0`, pièce jointe `<= 0` ou `> 50` pièces jointes, `partner_url` invalide/non-HTTP(S)/sans hôte/`> 512` chars, `visibility` hors `{0, 1}`, `expires_at` positif dans le passé (`<= nowMs`), média référencé inexistant/soft-deleted (`-1`), ou incohérence d'extension (ex: `.avif` dans `markdown_media_id` ou `.md` dans `cover_media_id`/`attachment_media_ids`).
// @Description    - Execution stage: `rejectQueryParameters`, `ShouldBindJSON`, `CreateArticleInput.ValidateAndNormalize`, `ValidateAndActivateAdminArticleMedia` ou `ArticlePayload.ValidateAndNormalize`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED / USER_NOT_IDENTIFIED / MISSING_TOKEN / INVALID_TOKEN / EXPIRED_TOKEN / MISSING_HEADERS] Authentification manquante ou invalide:**
// @Description    - Trigger: Token JWT absent/expiré/invalide, headers `X-Signature` / `X-Timestamp` manquants ou invalides, ou identifiant utilisateur absent du contexte.
// @Description    - Execution stage: `JWTMiddleware`, `HMACMiddleware`, `AdminGuard` ou `pkg.GetUserIDFromContext`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` / `numan_error.CodeUserIsNotIdentified` (ou code middleware JWT/HMAC exact).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Privilèges admin requis ou tentative d'utilisation d'un média privé utilisateur:**
// @Description    - Trigger: L'appelant n'a pas le grade Administrateur (`AdminGuard`), OU un média référencé (`cover_media_id`, `markdown_media_id`, `attachment_media_ids`) ne commence pas par le préfixe public `articles/` (ex: média privé `users/<id>/...`).
// @Description    - Execution stage: `AdminGuard` ou `ValidateAndActivateAdminArticleMedia`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  - **[MEDIA_REJECTED] Média rejeté par la modération:**
// @Description    - Trigger: Un des médias référencés possède le statut `MediaVisibilityRejected` (`3`).
// @Description    - Execution stage: `ValidateAndActivateAdminArticleMedia`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeMediaRejected`.
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] IP cliente hors sous-réseau VPN WireGuard:**
// @Description    - Trigger: Requête émise depuis une IP extérieure à `WIREGUARD_SUBNET` (`10.8.0.0/24` par défaut).
// @Description    - Execution stage: `AdminGuard`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  📦 **413 Payload Too Large:**
// @Description
// @Description  - **[PAYLOAD_TOO_LARGE] Corps JSON > 2 MiB:**
// @Description    - Trigger: Requête JSON dont la taille dépasse `2 MiB` (`2 097 152` octets).
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
// @Description  - **[CACHE_ERROR] Erreur Redis L1 ou échec de mise en file Write-Behind de l'article:**
// @Description    - Trigger: Client Redis nil, échec `HSet`/`ZAdd` dans `SetArticleInCache`, ou échec `redis.EnqueueDB` pour `EntityArticle` (suivi d'un rollback Redis L1).
// @Description    - Execution stage: `object_cache_service.SetArticleInCache` ou `CreateArticleService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Échec technique interne (activation média ou profil admin):**
// @Description    - Trigger: Échec `msgpack.Marshal`, échec `redis.EnqueueDB` lors de l'activation d'un média dans `ValidateAndActivateAdminArticleMedia`, ou erreur `GetUserLite` dans `AdminGuard`.
// @Description    - Execution stage: `AdminGuard`, `ValidateAndActivateAdminArticleMedia` ou `SetArticleInCache`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin,articles
// @Accept       json
// @Produce      json
// @Param        Authorization header string                            true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                            true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                            true "Timestamp Unix de la requête"
// @Param        input         body   article_models.CreateArticleInput true "Données de l'article à créer"
// @Success      200  {object}  article_models.CreateArticleOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only or non-article/rejected media)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (Outside WireGuard VPN)"
// @Failure      413  {object}  numan_error.PublicErrorResponse "Payload Too Large (> 2 MiB)"
// @Failure      429  {object}  numan_error.PublicErrorResponse "Too Many Requests"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Cache or Internal Server Error"
// @Router       /admin/articles/create [post]
func CreateArticleHandler(c *gin.Context) {
	if errQuery := rejectQueryParameters(c); errQuery != nil {
		numan_error.RespondWithError(c, errQuery)
		return
	}

	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input article_models.CreateArticleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	out, errSvc := article_service.CreateArticleService(c.Request.Context(), adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
