package article_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/article_service"
	"github.com/gin-gonic/gin"
)

// DeleteArticleHandler godoc
// @Summary      Supprimer logiquement un ou plusieurs articles Markdown (Admin)
// @Description  Permet exclusivement à un Administrateur de supprimer logiquement (`Visibility = -1`) un article unique ou un lot d'articles. Retire immédiatement les articles de l'index actif Redis L1, planifie le soft-delete Write-Behind vers MongoDB L2 et PostgreSQL L3, et marque en soft-delete différé (`MediaVisibilitySoftDeleted = -1`) uniquement les médias publics qui ne sont plus référencés par aucun autre article actif (nettoyage S3 différé après 48h par le garbage collector).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`), une signature HMAC valide (`X-Signature`, `X-Timestamp`) et un accès réseau VPN WireGuard (`WIREGUARD_SUBNET`, défaut `10.8.0.0/24`).
// @Description  - Required permissions or roles: Grade Administrateur strictement requis (`variables.UserGradeAdmin = 4` ou fallback `variables.MemberRoleAdmin = 1`), appliqué en amont par le middleware `AdminGuard` sur le groupe `/admin` dans `routes.go`.
// @Description  - Relevant middleware: `MaxBodySize` (limite JSON `2 MiB`), `RateLimiter` (50 req/10s/IP), `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`.
// @Description  - URL restrictions: Aucun paramètre de query string URL toléré (`rejectQueryParameters`).
// @Description
// @Description  **Request Contract:**
// @Description  - Route: `DELETE /admin/articles/delete` (les identifiants transitent exclusivement dans le corps JSON, jamais dans l'URL).
// @Description  - Body JSON (`article_models.DeleteArticleInput`):
// @Description    - `article_id` (int64, optionnel): identifiant d'article à supprimer (`> 0`).
// @Description    - `id` (int64, optionnel): alias de `article_id` (`> 0`).
// @Description    - `article_ids` ([]int64, optionnel): liste d'identifiants à supprimer en lot (chaque ID `> 0`, fusionnés et dédupliqués avec `article_id`/`id`). Au moins un ID positif doit être résolu.
// @Description    - `expected_updated_at` (*int64, optionnel): horodatage `> 0` de contrôle de concurrence optimiste. Strictement réservé à la suppression d'un seul article (`len(resolvedIDs) == 1`) ; rejeté en `400` si plusieurs IDs sont fournis.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Rejet des query parameters (`rejectQueryParameters`) et extraction de `adminID` (`pkg.GetUserIDFromContext`) après contrôle `AdminGuard`.
// @Description  2. Binding JSON dans `article_models.DeleteArticleInput` et résolution/déduplication des identifiants (`input.ResolvedIDs()`).
// @Description  3. Rejet `400` si `expected_updated_at != nil` avec plus d'un identifiant cible.
// @Description  4. Pour chaque identifiant cible :
// @Description     - Chargement via `object_cache_service.GetArticleFromCache` (cascade L1 -> L2 -> L3). En mode unitaire (`len(targetIDs) == 1`), une absence ou un article déjà supprimé retourne `404` ; en mode lot (`len(targetIDs) > 1`), les IDs absents sont ignorés sans bloquer les suppressions valides.
// @Description     - Vérification `expected_updated_at` si fourni (`409 Conflict` en cas de divergence).
// @Description     - Retrait synchrone de l'index actif Redis L1 (`DeleteArticleFromCache` : `HDEL` + `ZREM`).
// @Description     - Calcul d'un `UpdatedAt` strictement monotone, passage à `Visibility = -1` et mise en file Write-Behind (`redis.EnqueueDB` avec `EntityArticle`, `ActionDelete`, `TargetAll`). En cas d'échec de mise en file, restauration immédiate de `existing` dans Redis L1.
// @Description  5. Pour chaque article supprimé, appel best-effort de `media_service.DeactivateAdminMediaBatch` : vérifie les références croisées dans Redis L1 (`articles:index:data`) et PostgreSQL L3 (`FuncFilterUnreferencedArticleMedia`) puis marque uniquement les médias orphelins à `Visibility = -1` (aucune suppression physique S3 synchrone).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: `article_models.DeleteArticleOutput` (`deleted_count`, `article_ids`, `deactivated_media_ids`), réponse signée par `HMACMiddleware`.
// @Description  - Side effects: Disparition immédiate des articles de `ListArticlesHandler` et `GetArticleHandler`, synchronisation L2/L3 asynchrone et bascule des médias non partagés vers la file de purge GC différée (48h).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Identifiants manquants/invalides ou verrou optimiste en batch:**
// @Description    - Trigger: Query parameters dans l'URL, JSON malformé, aucun ID fourni, ID `<= 0` (négatif sur `article_id`/`id`, ou `<= 0` dans `article_ids`), `expected_updated_at <= 0`, ou utilisation de `expected_updated_at` avec plus d'un ID résolu.
// @Description    - Execution stage: `rejectQueryParameters`, `ShouldBindJSON`, `DeleteArticleInput.ResolvedIDs` ou vérification batch dans `DeleteArticleService`.
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
// @Description  - **[FORBIDDEN_ACCESS] Privilèges d'administrateur requis:**
// @Description    - Trigger: L'appelant authentifié ne possède pas le grade Administrateur (`UserGradeAdmin = 4` ou `MemberRoleAdmin = 1`).
// @Description    - Execution stage: Middleware `AdminGuard` (groupe `/admin`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Article unitaire introuvable / déjà supprimé, ou IP hors VPN WireGuard:**
// @Description    - Trigger: En suppression unitaire (`1` ID cible), l'article est absent des couches L1/L2/L3 ou déjà supprimé (`visibility == -1`), OU l'IP cliente est extérieure à `WIREGUARD_SUBNET`. (Note : en suppression par lot de `>= 2` IDs, les IDs introuvables sont silencieusement ignorés et ne déclenchent pas `404`).
// @Description    - Execution stage: `AdminGuard` ou `object_cache_service.GetArticleFromCache`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚠️ **409 Conflict:**
// @Description
// @Description  - **[RESOURCE_CONFLICT] Conflit de concurrence optimiste:**
// @Description    - Trigger: `expected_updated_at` ne correspond pas au `updated_at` actuel de l'article ciblé.
// @Description    - Execution stage: Contrôle de concurrence optimiste dans `DeleteArticleService`.
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
// @Description    - Trigger: Client Redis nil, échec `HDel`/`ZRem` dans `DeleteArticleFromCache`, ou échec `redis.EnqueueDB` pour `ActionDelete` (suivi d'une restauration dans Redis L1).
// @Description    - Execution stage: `DeleteArticleFromCache` ou `DeleteArticleService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Erreur technique interne:**
// @Description    - Trigger: Échec `GetUserLite` lors de la vérification du grade dans `AdminGuard`.
// @Description    - Execution stage: `AdminGuard`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin,articles
// @Accept       json
// @Produce      json
// @Param        Authorization header string                            true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                            true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                            true "Timestamp Unix de la requête"
// @Param        input         body   article_models.DeleteArticleInput true "Identifiant(s) d'article(s) à supprimer"
// @Success      200  {object}  article_models.DeleteArticleOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Article Not Found (single ID) or Outside WireGuard VPN"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Optimistic Concurrency Conflict"
// @Failure      413  {object}  numan_error.PublicErrorResponse "Payload Too Large (> 2 MiB)"
// @Failure      429  {object}  numan_error.PublicErrorResponse "Too Many Requests"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Cache or Internal Server Error"
// @Router       /admin/articles/delete [delete]
func DeleteArticleHandler(c *gin.Context) {
	if errQuery := rejectQueryParameters(c); errQuery != nil {
		numan_error.RespondWithError(c, errQuery)
		return
	}

	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input article_models.DeleteArticleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou identifiant d'article manquant.", err))
		return
	}

	out, errSvc := article_service.DeleteArticleService(c.Request.Context(), adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
