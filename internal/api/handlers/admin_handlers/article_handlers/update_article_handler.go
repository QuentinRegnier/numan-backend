package article_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/article_service"
	"github.com/gin-gonic/gin"
)

// UpdateArticleHandler godoc
// @Summary      Mettre à jour un article Markdown (Admin)
// @Description  Permet exclusivement à un Administrateur d'effectuer une mise à jour partielle ou complète d'un article existant, avec contrôle de concurrence optimiste optionnel via `expected_updated_at`. Re-valide et active les médias référencés sous `articles/`, garantit un horodatage `updated_at` strictement monotone pour l'ordonnancement Write-Behind, met à jour l'index Redis L1, planifie la synchronisation L2/L3 et calcule côté serveur les médias détachés afin de ne marquer en soft-delete différé (`-1`) que ceux qui ne sont plus utilisés par aucun autre article actif.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`), une signature HMAC valide (`X-Signature`, `X-Timestamp`) et un accès réseau VPN WireGuard (`WIREGUARD_SUBNET`, défaut `10.8.0.0/24`).
// @Description  - Required permissions or roles: Grade Administrateur strictement requis (`variables.UserGradeAdmin = 4` ou fallback `variables.MemberRoleAdmin = 1`), appliqué en amont par le middleware `AdminGuard` sur le groupe `/admin` dans `routes.go`.
// @Description  - Relevant middleware: `MaxBodySize` (limite JSON `2 MiB`), `RateLimiter` (50 req/10s/IP), `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`.
// @Description  - URL restrictions: Aucun paramètre de query string URL toléré (`rejectQueryParameters`).
// @Description
// @Description  **Request Contract:**
// @Description  - Route: `PUT /admin/articles/update` (l'ID transite exclusivement dans le corps JSON, jamais dans le path URL).
// @Description  - Body JSON (`article_models.UpdateArticleInput`):
// @Description    - `article_id` (int64, **requis** si `id` absent): identifiant strictement positif (`> 0`) ; alias accepté : `id`.
// @Description    - `title` (*string, optionnel): si fourni, nettoyé via `TrimSpace`, non vide et `<= 200` runes.
// @Description    - `summary` (*string, optionnel): si fourni, nettoyé via `TrimSpace`, `<= 500` runes (chaîne vide autorisée pour effacer).
// @Description    - `cover_media_id` (*int64, optionnel): si fourni, `>= 0` (`0` retire la couverture ; `> 0` vérifie extension `.avif`/`.gif`/`.webp` sous `articles/`).
// @Description    - `markdown_media_id` (*int64, optionnel): si fourni, doit être `> 0` et référencer un média `.md` sous `articles/`.
// @Description    - `attachment_media_ids` (*[]int64, optionnel): si fourni (y compris tableau vide `[]` pour tout retirer), IDs `> 0`, dédupliqués, max `50` éléments, extension `.avif`/`.gif`/`.webp` sous `articles/`.
// @Description    - `partner_url` (*string, optionnel): si fourni, chaîne vide autorisée ou URL HTTP/HTTPS valide `<= 512` caractères sans espace/caractère de contrôle.
// @Description    - `visibility` (*int, optionnel): `0` (`ArticleVisibilityPublished`) ou `1` (`ArticleVisibilityDraft`).
// @Description    - `expires_at` (*int64, optionnel): `>= 0` ; `0` efface la date d'expiration (`nil`) ; un nombre négatif est rejeté (`400`).
// @Description    - `clear_expires_at` (bool, optionnel): si `true`, force `expires_at = nil` (prioritaire sur `expires_at`).
// @Description    - `expected_updated_at` (*int64, optionnel): si fourni, doit être `> 0`. Déclenche un rejet `409 Conflict` si `expected_updated_at != existing.UpdatedAt`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Rejet des query parameters (`rejectQueryParameters`) et extraction de `adminID` (`pkg.GetUserIDFromContext`) après contrôle `AdminGuard`.
// @Description  2. Binding JSON dans `article_models.UpdateArticleInput` et validation partielle (`input.ValidateAndNormalize()`).
// @Description  3. Chargement de l'article cible via `object_cache_service.GetArticleFromCache` (cascade L1 Redis -> L2 MongoDB -> L3 PostgreSQL avec auto-réparation ; retourne `404` si absent ou soft-deleted).
// @Description  4. Contrôle de concurrence optimiste : si `expected_updated_at != nil && *expected_updated_at != existing.UpdatedAt`, retour immédiat `409 Conflict`.
// @Description  5. Fusion des champs modifiés et calcul d'un horodatage strictement monotone (`now = max(NowMillis(), existing.UpdatedAt + 1)`).
// @Description  6. Re-validation des invariants sur le payload fusionné (`updated.ValidateAndNormalize()`).
// @Description  7. Validation tout-ou-rien et activation cross-admin des médias référencés par la nouvelle version (`media_service.ValidateAndActivateAdminArticleMedia`).
// @Description  8. Calcul différentiel côté serveur des médias détachés (`diffRemovedMediaIDs`).
// @Description  9. Mise à jour synchrone de l'index Redis L1 (`object_cache_service.SetArticleInCache`).
// @Description  10. Planification Write-Behind (`redis.EnqueueDB` avec `EntityArticle`, `ActionUpdate`, `TargetAll`). En cas d'échec, restauration immédiate de l'état `existing` dans Redis L1.
// @Description  11. Désactivation différée best-effort (`media_service.DeactivateAdminMediaBatch`) des médias détachés non référencés par d'autres articles actifs en L1 ou PostgreSQL L3 (`Visibility = -1`, aucune suppression S3 synchrone — balayage différé > 48h par le worker GC).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: `article_models.UpdateArticleOutput` contenant `article` (`ArticleDetailView`) et `deactivated_media_ids` (liste des IDs médias passés en soft-delete, omise si vide), réponse signée par `HMACMiddleware`.
// @Description  - Side effects: Mise à jour immédiate dans Redis L1, synchronisation asynchrone L2/L3, et marquage différé éventuel des anciens médias orphelins.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Paramètres ou médias de mise à jour invalides:**
// @Description    - Trigger: Query parameters dans l'URL, JSON malformé, `article_id <= 0`, `title` vide ou `> 200` runes, `summary > 500` runes, `cover_media_id < 0`, `markdown_media_id <= 0`, pièce jointe `<= 0` ou `> 50`, `partner_url` invalide, `visibility` hors `{0, 1}`, `expires_at < 0`, `expected_updated_at <= 0`, média référencé introuvable/soft-deleted ou incohérence d'extension (`.md` vs visuel).
// @Description    - Execution stage: `rejectQueryParameters`, `ShouldBindJSON`, `UpdateArticleInput.ValidateAndNormalize`, `ArticlePayload.ValidateAndNormalize` ou `ValidateAndActivateAdminArticleMedia`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED / USER_NOT_IDENTIFIED / MISSING_TOKEN / INVALID_TOKEN / EXPIRED_TOKEN / MISSING_HEADERS] Authentification manquante ou invalide:**
// @Description    - Trigger: Token JWT absent/expiré/invalide, signature HMAC absente/invalide, ou `userID` absent du contexte.
// @Description    - Execution stage: `JWTMiddleware`, `HMACMiddleware`, `AdminGuard` ou `pkg.GetUserIDFromContext`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` / `numan_error.CodeUserIsNotIdentified` (ou code middleware exact).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Privilèges admin requis ou média privé non-article:**
// @Description    - Trigger: Appelant sans grade Administrateur (`AdminGuard`), OU rattachement d'un média ne portant pas le préfixe public `articles/`.
// @Description    - Execution stage: `AdminGuard` ou `ValidateAndActivateAdminArticleMedia`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  - **[MEDIA_REJECTED] Média rejeté par la modération:**
// @Description    - Trigger: Un média ciblé possède `Visibility == 3` (`MediaVisibilityRejected`).
// @Description    - Execution stage: `ValidateAndActivateAdminArticleMedia`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeMediaRejected`.
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Article introuvable / supprimé, ou IP hors VPN WireGuard:**
// @Description    - Trigger: L'article `article_id` n'existe dans aucune couche L1/L2/L3 ou est déjà supprimé logiquement (`visibility == -1`), OU l'IP cliente est extérieure à `WIREGUARD_SUBNET`.
// @Description    - Execution stage: `AdminGuard` ou `object_cache_service.GetArticleFromCache`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚠️ **409 Conflict:**
// @Description
// @Description  - **[RESOURCE_CONFLICT] Conflit de concurrence optimiste:**
// @Description    - Trigger: Le champ `expected_updated_at` fourni ne correspond pas au `updated_at` actuel de l'article stocké (modification concurrente survenue entre-temps).
// @Description    - Execution stage: Contrôle de concurrence optimiste dans `UpdateArticleService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
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
// @Description  - **[CACHE_ERROR] Échec Redis L1 ou Write-Behind:**
// @Description    - Trigger: Client Redis nil, échec `HSet`/`ZAdd` dans `SetArticleInCache`, ou échec `redis.EnqueueDB` pour `ActionUpdate` (suivi d'une restauration de `existing` dans Redis L1).
// @Description    - Execution stage: `object_cache_service.SetArticleInCache` ou `UpdateArticleService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Erreur technique interne:**
// @Description    - Trigger: Échec `msgpack.Marshal`, échec `redis.EnqueueDB` lors de l'activation d'un nouveau média, ou erreur `GetUserLite` dans `AdminGuard`.
// @Description    - Execution stage: `AdminGuard`, `ValidateAndActivateAdminArticleMedia` ou `SetArticleInCache`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin,articles
// @Accept       json
// @Produce      json
// @Param        Authorization header string                            true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                            true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                            true "Timestamp Unix de la requête"
// @Param        input         body   article_models.UpdateArticleInput true "Champs de l'article à mettre à jour"
// @Success      200  {object}  article_models.UpdateArticleOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only or non-article/rejected media)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Article Not Found (or outside WireGuard VPN)"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Optimistic Concurrency Conflict"
// @Failure      413  {object}  numan_error.PublicErrorResponse "Payload Too Large (> 2 MiB)"
// @Failure      429  {object}  numan_error.PublicErrorResponse "Too Many Requests"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Cache or Internal Server Error"
// @Router       /admin/articles/update [put]
func UpdateArticleHandler(c *gin.Context) {
	if errQuery := rejectQueryParameters(c); errQuery != nil {
		numan_error.RespondWithError(c, errQuery)
		return
	}

	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input article_models.UpdateArticleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	out, errSvc := article_service.UpdateArticleService(c.Request.Context(), adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
