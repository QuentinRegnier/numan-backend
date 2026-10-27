package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ReviewQuarantineHandler godoc
// @Summary      Statuer sur un média ou un post en quarantaine (Approbation / Rejet)
// @Description  Traite la décision arbitrale d'un modérateur sur un média en quarantaine et son post rattaché éventuel. Selon la décision rendue, le système met à jour atomiquement la visibilité des ressources, déclenche les cascades de purge ou de réindexation, et émet un événement asynchrone dans la file de réputation Redis pour recalculer le Trust Score de l'auteur :
// @Description  - `approve` : Visibilité média -> Public (1), post parent -> Public (0), ajout à la timeline profil L1 et émission d'un bonus de Trust Score (+0.05).
// @Description  - `reject_minor` : Visibilité média -> Rejeté (-2), post parent -> Modéré (-2), éviction du cache L1/L2 et émission d'un malus de Trust Score (-0.30).
// @Description  - `reject_major` : Visibilité média -> Rejeté (-2), post parent -> Modéré (-2), éviction immédiate et bascule du compte en Hard Ban (Trust Score mis à 0.00, révocation des sessions et bannissement de l'identifiant stéganographique).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements : Route privée protégée par `JWTMiddleware` et signature cryptographique tournante via `HMACMiddleware`.
// @Description  - Required permissions or roles : Le requérant doit disposer au minimum du grade Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware : MaxBodySize, RateLimiter, CORSMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, AdminGuard.
// @Description
// @Description  **Request Contract:**
// @Description  - Body format : JSON strict `moderation_models.ReviewQuarantineInput` envoyé dans le corps de la requête.
// @Description  - Required fields : `media_id` (int64, identifiant du média inspecté), `decision` (string, obligatoirement l'une des trois valeurs : `approve`, `reject_minor`, `reject_major`).
// @Description  - Optional fields : `post_id` (int64, identifiant de la publication conteneur associée), `reason` (string, max 500 caractères, motif de la décision).
// @Description  - Validation rules : Validation GIN stricte par balises de struct (`required,oneof=approve reject_minor reject_major`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Identification du modérateur appelant (`callerID`) et validation du corps de requête JSON.
// @Description  2. Contrôle Zero-Trust d'habilitation : validation du grade modérateur en RAM L1 (`cache_service.GetUserLite`).
// @Description  3. Localisation du média en cascade : consultation du Warm Storage L2 MongoDB (`mongo.MongoGetMediaByID`), puis Fallback L3 PostgreSQL (`postgres.FuncGetMedia`).
// @Description  4. Mutation de la visibilité du média (`MediaVisibilityPublic` ou `MediaVisibilityRejected`) et synchronisation L1 (mise à jour ou éviction de l'Object Cache).
// @Description  5. Persistance asynchrone du média vers les bases de données froides (Write-Behind via `redis.EnqueueDB` ciblant `TargetAll`).
// @Description  6. Si un `post_id` est rattaché :
// @Description     - En cas d'approbation : bascule du post en visibilité publique (`PostVisibilityAll`), réhydratation du cache L1 et insertion dans la timeline profil (`AddPostToUserProfile`).
// @Description     - En cas de rejet : bascule en visibilité modérée (`PostVisibilityModerated`), éviction du cache L1 (`DeletePostFromObjectCache`), purge de la timeline profil (`RemovePostFromUserProfile`) et envoi d'un ordre `ActionDelete` aux workers.
// @Description  7. Publication non bloquante de l'événement de réputation (`QUARANTINE_APPROVED`, `MINOR_INFRACTION`, ou `MAJOR_INFRACTION`) dans la file Redis `worker:queue:trust_score` consommée par le Worker de Réputation.
// @Description  8. Renvoi au client du DTO de confirmation `moderation_models.ReviewQuarantineOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status : 200 OK
// @Description  - Response body : JSON `moderation_models.ReviewQuarantineOutput` contenant l'ID du média, son nouvel état de visibilité numérique, l'événement de réputation émis et l'horodatage de mise à jour.
// @Description  - Persistence guarantees : Mutations appliquées immédiatement en RAM L1 (Object Cache & ZSETs), puis garanties en bases L2/L3 de manière transactionnelle par les Workers Write-Behind.
// @Description  - Side effects : Réindexation ou déréférencement du média/post, publication d'un travail de recalcul de réputation dans Redis, et en cas d'infraction majeure, révocation des sessions actives et enregistrement dans `moderation.stegano_bans`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON or validation failed:**
// @Description    - Trigger : Identifiant `media_id` manquant ou nul, décision absente ou différente de `approve`/`reject_minor`/`reject_major`, motif dépassant 500 caractères, ou JSON corrompu.
// @Description    - Execution stage : Validation du contrôleur GIN (`c.ShouldBindJSON`) ou contrôle métier au sein du service.
// @Description    - Response : `numan_error.PublicErrorResponse`.
// @Description    - Error code : `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing caller identity:**
// @Description    - Trigger : En-tête JWT manquant, altéré, expiré ou contexte d'appel non résolu.
// @Description    - Execution stage : Middleware d'authentification ou `pkg.GetUserIDFromContext`.
// @Description    - Response : `numan_error.PublicErrorResponse`.
// @Description    - Error code : `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Insufficient moderation privileges:**
// @Description    - Trigger : L'utilisateur connecté possède un grade inférieur à `UserGradeModerator` (3), ou son statut est banni/désactivé.
// @Description    - Execution stage : Évaluation des permissions au sein de `moderation_service.ReviewQuarantineMedia`.
// @Description    - Response : `numan_error.PublicErrorResponse`.
// @Description    - Error code : `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Media not found:**
// @Description    - Trigger : Le `media_id` ciblé n'existe ni dans le cache L2 MongoDB ni dans la source de vérité L3 PostgreSQL.
// @Description    - Execution stage : Phase de chargement en cascade du média.
// @Description    - Response : `numan_error.PublicErrorResponse`.
// @Description    - Error code : `numan_error.CodeNotFound`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Write-behind or queue enqueue failure:**
// @Description    - Trigger : Rupture de connexion avec Redis empêchant l'enfilement des mutations du média ou de l'événement de réputation dans la file du Worker.
// @Description    - Execution stage : Délégué Write-Behind (`redis.EnqueueDB`) ou publication d'événement (`PublishTrustScoreEvent`).
// @Description    - Response : `numan_error.PublicErrorResponse`.
// @Description    - Error code : `numan_error.CodeInternalError` ou `numan_error.CodeCacheError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                      true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                      true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                      true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ReviewQuarantineInput    true "Charge utile de la décision de modération"
// @Success      200  {object}  moderation_models.ReviewQuarantineOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Moderator or Admin required)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Media Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/quarantine/review [post]
func ReviewQuarantineHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input moderation_models.ReviewQuarantineInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou champs manquants.", err))
		return
	}

	output, errSvc := moderation_service.ReviewQuarantineMedia(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, output)
}
