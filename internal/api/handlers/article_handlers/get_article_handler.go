package article_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/article_service"
	"github.com/gin-gonic/gin"
)

// GetArticleHandler godoc
// @Summary      Récupérer les métadonnées détaillées d'un article
// @Description  Retourne les métadonnées complètes d'un article ainsi que les URLs publiques directes vers le fichier Markdown (`markdown_url`), l'image de couverture (`cover_url`) et les pièces jointes (`attachments`). Par sécurité et performance, le backend ne télécharge ni ne retourne jamais le contenu Markdown brut : le client télécharge directement les fichiers statiques depuis le stockage objet public / CDN. Les droits d'accès dépendent du rôle : un utilisateur standard ne voit que les articles publiés (`visibility == 0`) et non expirés ; un administrateur accède également aux brouillons (`visibility == 1`) et aux articles expirés. Les articles supprimés logiquement (`visibility == -1`) renvoient toujours `404`.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`) et une signature HMAC valide (`X-Signature`, `X-Timestamp`).
// @Description  - Routes et rôles:
// @Description    - `POST /articles/get`: accessible à tout utilisateur authentifié (et aux administrateurs qui y voient également brouillons/expirés via `isCallerAdmin`).
// @Description    - `POST /admin/articles/get`: alias monté sur `adminGroup`, protégé en amont par `AdminGuard` (VPN WireGuard `WIREGUARD_SUBNET` + grade Administrateur `4` ou `1`).
// @Description  - Relevant middleware: `MaxBodySize` (limite JSON `2 MiB`), `RateLimiter` (50 req/10s/IP), `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware` (et `AdminGuard` sur `/admin/articles/get`).
// @Description  - URL restrictions: Aucun paramètre de query string URL toléré (`rejectQueryParameters`), et aucun ID d'article dans le path URL.
// @Description
// @Description  **Request Contract:**
// @Description  - Routes: `POST /articles/get` (utilisateur / admin) et `POST /admin/articles/get` (admin sous WireGuard).
// @Description  - Body JSON (`article_models.GetArticleInput`):
// @Description    - `article_id` (int64, **requis** si `id` absent): identifiant strictement positif (`> 0`).
// @Description    - `id` (int64, optionnel): alias accepté pour `article_id` (`> 0`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Rejet des query parameters (`rejectQueryParameters`) et extraction de `callerID` (`pkg.GetUserIDFromContext`).
// @Description  2. Binding JSON dans `article_models.GetArticleInput` et validation (`input.Validate()`, `ResolvedID() > 0`).
// @Description  3. Détermination du rôle administrateur via `isCallerAdmin` (`user_grade` du contexte ou lookup `UsersLite` en cache L1).
// @Description  4. Chargement de l'article en cascade via `object_cache_service.GetArticleFromCache` : Redis L1 (`HGet articles:index:data`) -> MongoDB L2 -> PostgreSQL L3 (avec auto-réparation des couches supérieures en cas de miss).
// @Description  5. Filtrage strict de visibilité :
// @Description     - Si `article.Visibility < 0` (soft-deleted) : retourne toujours `404 RESOURCE_NOT_FOUND`.
// @Description     - Si `!isAdmin` et (`article.Visibility != 0` ou `article.IsExpired(nowMs)`) : retourne également `404 RESOURCE_NOT_FOUND` (et non `403`, afin de ne pas révéler l'existence d'un brouillon ou d'un article expiré).
// @Description  6. Hydratation des médias en un batch Redis (`ResolvePublicMediaMapCascade` -> fallback L2/L3 + génération d'URL publique via `PUBLIC_MEDIA_BASE_URL` sans appel réseau S3 ni lecture du `.md`).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: `article_models.GetArticleOutput` encapsulant `ArticleDetailView` (`id`, `title`, `summary`, `cover_media_id`, `cover_url`, `markdown_media_id`, `markdown_url`, `attachments`, `partner_url`, `visibility`, `expires_at`, `created_at`, `updated_at`), réponse signée par `HMACMiddleware`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Query parameters interdits, JSON malformé ou identifiant invalide:**
// @Description    - Trigger: Présence de query parameters dans l'URL, JSON malformé, ou `article_id` / `id` absent ou `<= 0`.
// @Description    - Execution stage: `rejectQueryParameters`, `pkg.GetUserIDFromContext` (type `userID` invalide), `ShouldBindJSON`, ou `GetArticleInput.Validate`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED / USER_NOT_IDENTIFIED / MISSING_TOKEN / INVALID_TOKEN / EXPIRED_TOKEN / MISSING_HEADERS] Authentification manquante ou invalide:**
// @Description    - Trigger: Token JWT absent/expiré/invalide, headers `X-Signature` / `X-Timestamp` manquants ou invalides, ou `userID` absent du contexte.
// @Description    - Execution stage: `JWTMiddleware`, `HMACMiddleware`, `AdminGuard` (sur `/admin/articles/get`) ou `pkg.GetUserIDFromContext`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` / `numan_error.CodeUserIsNotIdentified` (ou code middleware exact).
// @Description
// @Description  ⛔ **403 Forbidden (uniquement sur l'alias `/admin/articles/get`):**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Privilèges d'administrateur requis:**
// @Description    - Trigger: Requête envoyée sur `/admin/articles/get` (depuis le VPN WireGuard) par un utilisateur non administrateur. (Note : la route `/articles/get` ne renvoie jamais `403`, les brouillons et articles expirés y renvoient `404`).
// @Description    - Execution stage: `AdminGuard` sur `adminGroup`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Article introuvable, supprimé, brouillon/expiré (non-admin), ou IP hors VPN (route `/admin`):**
// @Description    - Trigger: Aucun article trouvé en L1/L2/L3, article soft-deleted (`visibility == -1`), appelant non-admin demandant un brouillon (`visibility == 1`) ou un article expiré (`expires_at <= nowMs`), OU IP hors `WIREGUARD_SUBNET` sur l'alias `/admin/articles/get`.
// @Description    - Execution stage: `AdminGuard` (alias admin), `GetArticleFromCache` ou filtrage de visibilité dans `GetArticleService`.
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
// @Description  - **[INTERNAL_SERVER_ERROR] Erreur technique de cache ou profil utilisateur:**
// @Description    - Trigger: Échec `GetUserLite` dans `AdminGuard` sur `/admin/articles/get` (la cascade lecture `GetArticleFromCache` absorbe en revanche les miss L1/L2 vers L3).
// @Description    - Execution stage: `AdminGuard`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         articles
// @Accept       json
// @Produce      json
// @Param        Authorization header string                         true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                         true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                         true "Timestamp Unix de la requête"
// @Param        input         body   article_models.GetArticleInput true "Identifiant de l'article"
// @Success      200  {object}  article_models.GetArticleOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (on /admin/articles/get only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Article Not Found (or Draft/Expired for non-admin)"
// @Failure      413  {object}  numan_error.PublicErrorResponse "Payload Too Large (> 2 MiB)"
// @Failure      429  {object}  numan_error.PublicErrorResponse "Too Many Requests"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /articles/get [post]
func GetArticleHandler(c *gin.Context) {
	if errQuery := rejectQueryParameters(c); errQuery != nil {
		numan_error.RespondWithError(c, errQuery)
		return
	}

	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input article_models.GetArticleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou identifiant d'article manquant.", err))
		return
	}

	isAdmin := isCallerAdmin(c, callerID)
	out, errSvc := article_service.GetArticleService(c.Request.Context(), callerID, isAdmin, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
