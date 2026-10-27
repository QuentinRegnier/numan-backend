package post_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/gin-gonic/gin"
)

// CreatePostHandler godoc
// @Summary      Créer une nouvelle publication
// @Description  Crée un nouveau post (publication) pour l'utilisateur, incluant la vectorisation sémantique locale, la mise en cache immédiate et la délégation asynchrone vers la base de données.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT d'authentification valide.
// @Description  - Required permissions or roles: L'utilisateur appelant doit être identifié par le middleware (Token valide).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction et validation des entrées :**
// @Description     - Extraction de l'identité de l'appelant via `pkg.GetUserIDFromContext`.
// @Description     - Binding GIN du payload JSON.
// @Description     - Nettoyage des chaînes (`pkg.CleanStr`), déduplication des hashtags/identifiants (`pkg.SliceUnique`).
// @Description     - Rejet immédiat si le texte et les médias sont tous deux vides, ou si la limite de médias (4) est dépassée.
// @Description  2. **Création métier :**
// @Description     - Génération d'un ID Snowflake distribué.
// @Description     - Assemblage conditionnel des hashtags (ajout du tag auteur si absent) et calcul du niveau de priorité (`priorityLevel`).
// @Description  3. **Intelligence Artificielle et Cache Synchrone :**
// @Description     - Exécution synchrone (O(1)) de `algorithm_service.ComputeContentVectorFull` pour générer l'empreinte sémantique (Vectorisation locale).
// @Description     - Écriture instantanée dans le cache Objet JSON L1 (`object_cache_service.SetPostInObjectCache`) et ajout de l'ID dans la timeline (ZSET) de l'utilisateur.
// @Description  4. **Persistance Asynchrone et Réponse :**
// @Description     - Envoi du post en file d'attente (Write-Behind) via `redis.EnqueueDB` (ActionCreate, TargetAll) pour l'insertion différée dans Postgres/Mongo.
// @Description     - Renvoi immédiat HTTP 201 Created avec l'identifiant du nouveau post généré.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Requête mal formatée ou paramètres invalides:**
// @Description    - Trigger: Format JSON invalide, payload vide (sans texte ni média), ou dépassement de la limite de 4 médias par post.
// @Description    - Execution stage: Validation du contrôleur GIN et règles métier synchrones initiales.
// @Description    - Response: `numan_error.PublicErrorResponse` avec détails, ou map JSON selon le format du routeur HTTP.
// @Description    - Error code: `numan_error.CodeInvalidPayload`
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur de service inattendue:**
// @Description    - Trigger: Échec critique de l'enfilement `redis.EnqueueDB` rendant la persistance asynchrone impossible.
// @Description    - Execution stage: Exécution profonde du service métier.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`
// @Tags         posts
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   post_models.CreatePostInput true "Payload pour créer un post"
// @Success      200  {object}  post_models.CreatePostResponse
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /posts/set [post]
func CreatePostHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input post_models.CreatePostInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Invalid JSON ou validation échouée.", err))
		return
	}

	postID, err := post_service.CreatePost(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	forceQuarantine := c.GetBool("force_quarantine")
	effectiveVis := input.Visibility
	if forceQuarantine {
		effectiveVis = 3 // variables.PostVisibilityQuarantine
	}

	resp := post_models.CreatePostResponse{
		PostID:             postID,
		Visibility:         effectiveVis,
		RequiresModeration: forceQuarantine,
	}
	if forceQuarantine {
		resp.Message = "Compte tenu du fait que votre compte est récent, votre publication a été placée en file de vérification avant d'apparaître dans le flux public."
	}

	c.JSON(http.StatusCreated, resp)
}
