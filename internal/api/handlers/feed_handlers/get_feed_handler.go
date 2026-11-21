package feed_handlers

import (
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/gin-gonic/gin"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/feed_models"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/feed_service"
)

// GetFeedHandler godoc
// @Summary      Récupérer le fil d'actualité
// @Description  Récupère un lot de publications pour le fil d'actualité (Feed) de l'utilisateur de manière algorithmique et personnalisée.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT requis).
// @Description  - Required permissions: Profil de télémétrie valide et score de confiance suffisant.
// @Description  - Relevant middleware: HMAC Validator, RateLimiter, JWT Auth.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `last_seen_index` (int, envoyé dans le corps JSON, 0 pour un premier appel).
// @Description  - Optional fields: Le booléen `force` est déduit automatiquement si l'URL se termine par `/force`.
// @Description  - Validation rules: Binding JSON classique.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation & Extraction** : Vérification du payload JSON et identification de l'appelant. Détection du mode `force` depuis le path de l'URL.
// @Description  2. **Profilage** : Chargement du graphe social rapide et de l'ADN algorithmique (télémétrie L1). Refus 404 si le profil n'est pas synchronisé.
// @Description  3. **Distribution** : Lancement de l'algorithme `HandlePullToRefresh` pour récupérer les IDs de publication depuis des tampons tournants (A/B/C) selon des quotas.
// @Description  4. **Hydratation Sécurisée** : Chargement riche des posts, avec filtrage des trous de visibilité et vérification absolue des permissions métier et blocages (Relation L1/L2).
// @Description  5. **Pagination** : Mise à jour incrémentale de l'index de pagination `LastSeenIndex`.
// @Description  6. **Réponse** : Retourne les posts hydratés, le nouvel index et le tampon actif utilisé (utile pour le debug client).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `feed_models.GetFeedOutput`.
// @Description  - Persistence guarantees: Lecture et hydratation principalement via ZSET Redis et Object Cache.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request — Payload invalide**
// @Description  - **Trigger:** Le payload JSON de la requête est absent, mal formaté ou ne respecte pas les contraintes de validation du modèle.
// @Description  - **Execution stage:** Phase de binding GIN et validation initiale des entrées, avant l'exécution de la logique métier.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeInvalidPayload` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  🟠 **401 Unauthorized — Authentification échouée**
// @Description  - **Trigger:** Le token JWT est absent, invalide ou le contexte utilisateur est introuvable.
// @Description  - **Execution stage:** Middleware d'authentification ou fonction `pkg.GetUserIDFromContext`.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeUnauthorized` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  ⚫ **404 Not Found — Aucun Post ou Télémétrie absente**
// @Description  - **Trigger:** Soit le profil de télémétrie de l'utilisateur n'est pas synchronisé (`ConfidenceScore == 0.0`), soit aucun post visible n'a pu être trouvé après hydratation (ZSET épuisé).
// @Description  - **Execution stage:** Vérification du profil en début de service, ou après la boucle d'hydratation.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeTelemetrySyncRequired` (si télémétrie absente) ou `numan_error.CodeNotFound` (si feed vide).
// @Description
// @Description  ⚫ **500 Internal Server Error — Erreur inattendue**
// @Description  - **Trigger:** Échec de la communication avec les caches (Redis) ou la base de données (MongoDB/PostgreSQL).
// @Description  - **Execution stage:** Exécution de la logique algorithmique et d'hydratation dans le service.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeInternalError` (renvoyé dans le champ JSON `code`).
// @Tags         feed
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   feed_models.GetFeedInput true "Payload du renouvellement du feed (last_seen_index, etc.)"
// @Success      200  {object}  feed_models.GetFeedOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /feed/get [post]
func GetFeedHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input feed_models.GetFeedInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	if strings.HasSuffix(c.Request.URL.Path, "/force") || strings.EqualFold(c.Query("force"), "true") {
		input.Force = true
	}

	postOutput, endIndex, activeFeed, err := feed_service.GetFeed(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, feed_models.GetFeedOutput{
		Status:        "Feed généré et hydraté avec succès",
		ActiveFeed:    activeFeed,
		LastSeenIndex: endIndex,
		Posts:         postOutput,
	})
}
