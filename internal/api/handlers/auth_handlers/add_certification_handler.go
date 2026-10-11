package auth_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/auth_service"
	"github.com/gin-gonic/gin"
)

// AddCertificationHandler godoc
// @Summary      Demander une certification auprès d'une fédération
// @Description  Permet à un utilisateur de se faire certifier en envoyant son ID de licence auprès d'une fédération sportive reconnue.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC.
// @Description  - Required permissions or roles: L'utilisateur doit être authentifié (le `userID` est extrait du contexte).
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS, Recovery.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `prestataire` (string) et `certification_id` (string) transmis dans le corps JSON.
// @Description  - Fédérations acceptées (Prestataires valides pour tests) : `federation_francaise`, `federation_allemande`, `federation_espagnole`, `federation_italienne`.
// @Description  - Validation rules: Binding GIN standard sur `auth_models.AddCertificationInput`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction du `userID` depuis le contexte sécurisé de la requête.
// @Description  2. Validation du payload JSON pour récupérer le `prestataire` et le `certification_id`.
// @Description  3. Simulation de l'appel HTTP externe vers l'API de la fédération (Vérification du retour 200 OK).
// @Description  4. Recherche de l'utilisateur courant (Cascade L2 MongoDB -> L3 PostgreSQL).
// @Description  5. Vérification de l'absence du badge pour éviter les doublons.
// @Description  6. Ajout du badge (ex: `certification_federation_francaise`) au profil de l'utilisateur.
// @Description  7. Éviction immédiate de l'utilisateur du cache L1 (RAM) via `redis.UsersLite.DeleteObject` pour forcer la réhydratation.
// @Description  8. Mise en file d'attente asynchrone (Write-Behind vers Redis) pour persister l'utilisateur sur L2 (Mongo) et L3 (Postgres).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `auth_models.AddCertificationOutput` contenant un message de succès et la nouvelle liste de badges.
// @Description  - Persistence guarantees: L'invalidation en RAM est synchrone. L'écriture physique en BDD est asynchrone (Write-Behind).
// @Description  - Side effects: Badge attribué, cache L1 purgé, worker Redis mobilisé.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Invalid JSON or validation failure:**
// @Description    - Trigger: Le champ `prestataire` ou `certification_id` est absent ou mal formaté.
// @Description    - Execution stage: Validation GIN.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN] API Federation validation failed:**
// @Description    - Trigger: Le prestataire n'est pas dans la whitelist ou la fédération a rejeté le numéro de licence.
// @Description    - Execution stage: Simulation de l'appel HTTP externe.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field).
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[NOT_FOUND] User profile not found:**
// @Description    - Trigger: L'utilisateur est introuvable en L2 (Mongo) et L3 (Postgres).
// @Description    - Execution stage: Chargement du profil utilisateur.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound` (returned in the `code` field).
// @Description
// @Description  🟡 **409 Conflict:**
// @Description
// @Description  - **[CONFLICT] Badge already exists:**
// @Description    - Trigger: L'utilisateur possède déjà la certification demandée pour cette fédération.
// @Description    - Execution stage: Vérification et attribution du badge.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict` (returned in the `code` field).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Queue failure:**
// @Description    - Trigger: Échec critique lors de l'insertion dans la file d'attente Redis (Write-Behind).
// @Description    - Execution stage: Mise en file d'attente.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field).
// @Tags         auth, profile
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   auth_models.AddCertificationInput true "Données de la licence et de la fédération"
// @Success      200  {object}  auth_models.AddCertificationOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Federation validation failed"
// @Failure      404  {object}  numan_error.PublicErrorResponse "User Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Badge already exists"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /certification [post]
func AddCertificationHandler(c *gin.Context) {
	// 1. Extraction sécurisée de l'identité
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	// 2. Lecture et validation stricte du JSON
	var input auth_models.AddCertificationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les données de certification (JSON) sont invalides.", err))
		return
	}

	// 3. Délégation totale au Service Métier (DDD)
	output, svcErr := auth_service.AddCertification(c.Request.Context(), callerID, input)
	if svcErr != nil {
		numan_error.RespondWithError(c, svcErr)
		return
	}

	// 4. Réponse HTTP
	c.JSON(http.StatusOK, output)
}
