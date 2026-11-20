package article_handlers

import (
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/gin-gonic/gin"
)

// UploadArticleAssetHandler godoc
// @Summary      Téléverser un asset public d'article (Admin)
// @Description  Permet exclusivement à un Administrateur de téléverser un asset public immuable destiné aux articles Markdown (image statique convertie en AVIF 1024px ou 2048px, GIF animé conservé brut, WebP animé conservé brut, ou document Markdown UTF-8 strictement assaini) dans le bucket S3 public (`numan-public`) sous `articles/<uuid>.<ext>`. Le média est provisionné en cache Redis L1 avec visibilité `MediaVisibilityPending` (`0`) et enfilé en Write-Behind (`ActionCreate`, `TargetAll`) jusqu'à son activation lors de la création ou mise à jour d'un article.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT valide (`Authorization: Bearer <jwt>`), une signature HMAC valide (`X-Signature`, `X-Timestamp`) et un accès réseau VPN WireGuard (`WIREGUARD_SUBNET`, défaut `10.8.0.0/24`).
// @Description  - Required permissions or roles: Grade Administrateur strictement requis (`variables.UserGradeAdmin = 4` ou fallback `variables.MemberRoleAdmin = 1`), appliqué en amont par le middleware `AdminGuard` sur le groupe `/admin` dans `routes.go`.
// @Description  - Relevant middleware: `MaxBodySize` (limite multipart globale à `15 MiB` = 15 728 640 octets), `RateLimiter` (50 req/10s/IP), `CORSMiddleware`, `gin.Recovery`, `JWTMiddleware`, `HMACMiddleware`, `AdminGuard`.
// @Description  - URL restrictions: Tout paramètre de query string (`?foo=bar`) est strictement interdit (`rejectQueryParameters`).
// @Description
// @Description  **Request Contract:**
// @Description  - Routes: `POST /admin/articles/media/upload` (et alias `POST /admin/articles/upload`).
// @Description  - Content-Type: `multipart/form-data`.
// @Description  - Required form field: `file` (binaire ou texte).
// @Description  - Optional form fields:
// @Description    - `kind` (ou alias `asset_kind`): `'image'` (défaut) ou `'markdown'` (ou `'md'`). Si omis, auto-détecté sur suffixe `.md`/`.markdown` du nom de fichier, sinon `'image'`.
// @Description    - `quality` (ou alias `quality_tier`): `'standard'` / `'1024'` (défaut, dimension max `1024px`) ou `'high'` / `'2048'` (dimension max `2048px`). Validé même pour les fichiers Markdown.
// @Description  - Format & Quota Rules:
// @Description    - Markdown (`kind=markdown`): max `256 Ko` (`262 144` octets), UTF-8 valide obligatoire, aucun octet `0x00` ni caractère de contrôle binaire (hors `\t`, `\n`, `\r`), rejet des magic bytes binaires (ELF, MZ/PE, Mach-O, PDF, ZIP, PNG, JPEG, GIF, RIFF), rejet des préfixes HTML/SVG/XML autonomes (`<!doctype html`, `<html`, `<svg`, `<?xml`, `<script`, `<iframe`, `<object`, `<embed`) et des schémas `javascript:` / `vbscript:`. Stocké brut sans modification sous `.md` (`text/markdown; charset=utf-8`).
// @Description    - GIF (`GIF87a` / `GIF89a`): max `5 Mo` (`5 242 880` octets), max `8192x8192` pixels (`67 108 864` px), structure validée via `gif.DecodeAll` (au moins 1 trame), conservé brut sous `.gif` (`image/gif`).
// @Description    - WebP (`RIFF....WEBP`): max `8192x8192` pixels ; si drapeau `VP8X` animé / chunks `ANIM`/`ANMF`, max `5 Mo` (`5 242 880` octets) et conservé brut sous `.webp` (`image/webp`) ; si WebP statique, transcodé en `.avif`.
// @Description    - Images statiques (JPEG, PNG, WebP statique, AVIF `ftypavif`/`ftypavis`/`ftypmif1`): max `15 Mo` (`15 728 640` octets), max `8192x8192` pixels, redimensionnement homothétique sans upscaling ni rognage (`imaging.Fit` + Lanczos selon `quality`), encodage final en `.avif` (`image/avif`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Rejet immédiat si la query string URL est non vide (`rejectQueryParameters`).
// @Description  2. Extraction de `adminID` (`pkg.GetUserIDFromContext`) après passage par `JWTMiddleware`, `HMACMiddleware` et `AdminGuard`.
// @Description  3. Extraction et ouverture du flux multipart `file` (`c.FormFile("file")` puis `fileHeader.Open()`), puis lecture des champs `quality`/`quality_tier` et `kind`/`asset_kind`.
// @Description  4. Génération d'un ID Snowflake (`pkg.GenerateID`) et délégation à `media_service.UploadAdminArticleAsset`.
// @Description  5. Validation du `kind`, résolution du `quality` (`1024px` ou `2048px`), lecture bornée (`readBoundedStream`) et validation/transcodage selon le type d'asset.
// @Description  6. Upload synchrone sur le bucket S3 public `numan-public` au chemin `articles/<uuid>.<ext>`.
// @Description  7. Provisionnement en cache L1 Redis (`object_cache_service.SetMediaInObjectCache` avec `Visibility = 0` / `MediaVisibilityPending`) — rollback S3 (`RemoveObject`) si `Validate()` ou Redis L1 échoue.
// @Description  8. Planification Write-Behind asynchrone (`redis.EnqueueDB` avec `EntityMedia`, `ActionCreate`, `TargetAll`) — rollback Redis L1 + S3 si l'enfilement échoue.
// @Description  9. Retour HTTP `200 OK` avec `media_id`, `storage_path`, `url` publique (`PUBLIC_MEDIA_BASE_URL`) et `content_type`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: `200 OK`
// @Description  - Response body: `media_models.UploadAdminArticleAssetOutput` (`media_id`, `storage_path`, `url`, `content_type`), réponse signée par `HMACMiddleware`.
// @Description  - Persistence guarantees: Objet présent sur S3 `numan-public`, métadonnées présentes en cache Redis L1 et persistées en Write-Behind vers MongoDB L2 et PostgreSQL L3.
// @Description  - Side effects: Le média reste `Pending` (`0`) jusqu'à son rattachement à un article via `CreateArticleHandler` ou `UpdateArticleHandler`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Query parameters interdits, options invalides ou fichier non conforme:**
// @Description    - Trigger: Présence d'un paramètre dans l'URL (`?x=1`), `kind` différent de `'image'`/`'markdown'`/`'md'`, `quality` différent de `'standard'`/`'1024'`/`'high'`/`'2048'`, fichier vide (`0` octet), Markdown binaire/non-UTF-8/NUL/contrôle/HTML/SVG/XSS, format image non supporté ou en-tête GIF/WebP/JPEG/PNG/AVIF corrompu.
// @Description    - Execution stage: `rejectQueryParameters`, `pkg.GetUserIDFromContext` (type `userID` invalide), ou validation dans `media_service.UploadAdminArticleAsset`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  - **[MISSING_FILE] Champ multipart `file` absent:**
// @Description    - Trigger: Le champ form-data `file` est absent ou la requête multipart est tronquée/incomplète (y compris après coupure TCP par `http.MaxBytesReader` au-delà de `15 MiB`).
// @Description    - Execution stage: `c.FormFile("file")` ou vérification de flux non nil.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeMissingFile`.
// @Description
// @Description  - **[FILE_READ_ERROR] Erreur d'ouverture ou de lecture du flux fichier:**
// @Description    - Trigger: Échec de `fileHeader.Open()` ou erreur I/O pendant `io.ReadAll` (ex: flux réseau interrompu / dépassement `http.MaxBytesReader`).
// @Description    - Execution stage: `fileHeader.Open()` dans le handler ou `readBoundedStream` dans le service.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeFileReadError`.
// @Description
// @Description  - **[PAYLOAD_TOO_LARGE] Quota applicatif du fichier ou résolution pixels dépassée:**
// @Description    - Trigger: Markdown > `256 Ko`, GIF animé > `5 Mo`, WebP animé > `5 Mo`, image statique > `15 Mo`, ou résolution > `8192x8192` pixels (`67 108 864` px).
// @Description    - Note importante pour les testeurs API : Ces dépassements détectés au niveau du service applicatif utilisent `numan_error.NewBadRequest` et renvoient donc le statut HTTP **400** avec le code JSON `PAYLOAD_TOO_LARGE` (à distinguer du HTTP **413** renvoyé par le middleware `MaxBodySize` lorsque le header `Content-Length > 15 MiB`).
// @Description    - Execution stage: `readBoundedStream`, `validateImageDimensions` ou `processAndValidateVisualArticleAsset`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodePayloadTooLarge`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED / USER_NOT_IDENTIFIED / MISSING_TOKEN / INVALID_TOKEN / EXPIRED_TOKEN / MISSING_HEADERS] Authentification manquante ou invalide:**
// @Description    - Trigger: Token JWT absent/expiré/invalide, headers `X-Signature` / `X-Timestamp` manquants ou invalides, ou clé `userID` absente du contexte Gin.
// @Description    - Execution stage: `JWTMiddleware`, `HMACMiddleware`, `AdminGuard` ou `pkg.GetUserIDFromContext` (`USER_NOT_IDENTIFIED`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` / `numan_error.CodeUserIsNotIdentified` (ou codes JWT/HMAC spécifiques : `MISSING_TOKEN`, `INVALID_SIGNING_ALGORITHM`, `INVALID_TOKEN`, `INVALID_CLAIMS`, `EXPIRED_TOKEN`, `OBSOLETE_TOKEN`, `MISSING_HEADERS`, `MISSING_CONTEXT`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Privilèges d'administrateur requis:**
// @Description    - Trigger: L'utilisateur authentifié (dans la plage VPN WireGuard) ne possède pas le grade Administrateur (`UserGradeAdmin = 4` ou `MemberRoleAdmin = 1`).
// @Description    - Execution stage: Middleware `AdminGuard` (groupe `/admin`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] IP cliente hors sous-réseau VPN WireGuard:**
// @Description    - Trigger: Requête admin émise depuis une IP extérieure à `WIREGUARD_SUBNET` (`10.8.0.0/24` par défaut). `AdminGuard` renvoie volontairement `404` pour dissimuler l'existence des routes admin.
// @Description    - Execution stage: `AdminGuard` (contrôle IP réseau).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  📦 **413 Payload Too Large:**
// @Description
// @Description  - **[PAYLOAD_TOO_LARGE] Taille brute de la requête HTTP multipart > 15 MiB:**
// @Description    - Trigger: Header HTTP `Content-Length` strictement supérieur à `15 MiB` (`15 728 640` octets).
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
// @Description  - **[INTERNAL_SERVER_ERROR] Échec S3 MinIO, encodage AVIF, cache L1, file Write-Behind ou client non initialisé:**
// @Description    - Trigger: `minio.MinioClient == nil`, erreur `Seek`, erreur décodage/encodage image AVIF, échec `PutObject` S3, échec `SetMediaInObjectCache` (avec rollback S3), échec `EnqueueDB` (avec rollback L1 + S3), ou échec `GetUserLite` dans `AdminGuard`.
// @Description    - Execution stage: `AdminGuard` ou `media_service.UploadAdminArticleAsset`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin,articles
// @Accept       multipart/form-data
// @Produce      json
// @Param        Authorization header   string true  "Bearer <votre_jwt>"
// @Param        X-Signature   header   string true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header   string true  "Timestamp Unix de la requête"
// @Param        file          formData file   true  "Fichier asset (JPEG, PNG, WebP statique/animé, GIF animé, AVIF, ou Markdown UTF-8)"
// @Param        kind          formData string false "Type explicite d'asset : 'image' (défaut) ou 'markdown' (alias: asset_kind)"
// @Param        quality       formData string false "Palier de résolution image : 'standard'/'1024' (1024px, défaut) ou 'high'/'2048' (2048px) (alias: quality_tier)"
// @Success      200  {object}  media_models.UploadAdminArticleAssetOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload, Missing File, File Read Error, or Service Quota Exceeded"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found (Outside WireGuard VPN)"
// @Failure      413  {object}  numan_error.PublicErrorResponse "Payload Too Large (> 15 MiB)"
// @Failure      429  {object}  numan_error.PublicErrorResponse "Too Many Requests"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/articles/media/upload [post]
func UploadArticleAssetHandler(c *gin.Context) {
	if errQuery := rejectQueryParameters(c); errQuery != nil {
		numan_error.RespondWithError(c, errQuery)
		return
	}

	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeMissingFile, "Fichier manquant ou invalide.", err))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeFileReadError, "Impossible de lire le fichier téléversé.", err))
		return
	}
	defer func(f multipart.File) {
		if errClose := f.Close(); errClose != nil {
			numan_log.Error(c.Request.Context()).Err(errClose).Msg("Erreur lors de la fermeture du fichier d'article uploadé")
		}
	}(file)

	qualityTier := strings.TrimSpace(c.PostForm("quality"))
	if qualityTier == "" {
		qualityTier = strings.TrimSpace(c.PostForm("quality_tier"))
	}

	assetKind := strings.TrimSpace(c.PostForm("kind"))
	if assetKind == "" {
		assetKind = strings.TrimSpace(c.PostForm("asset_kind"))
	}
	if assetKind == "" {
		lowerName := strings.ToLower(strings.TrimSpace(fileHeader.Filename))
		if strings.HasSuffix(lowerName, ".md") || strings.HasSuffix(lowerName, ".markdown") {
			assetKind = "markdown"
		} else {
			assetKind = "image"
		}
	}

	mediaID := pkg.GenerateID()
	out, errUpload := media_service.UploadAdminArticleAsset(
		c.Request.Context(),
		file,
		adminID,
		mediaID,
		assetKind,
		qualityTier,
	)
	if errUpload != nil {
		numan_error.RespondWithError(c, errUpload)
		return
	}

	c.JSON(http.StatusOK, out)
}
