package conversation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
	"github.com/gin-gonic/gin"
)

// SuggestConversationHandler godoc
// @Summary      Suggérer des membres pour un groupe
// @Description  Renvoie une liste restreinte de profils pertinents pouvant être ajoutés à la conversation ciblée.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT requis).
// @Description  - Security checks: L'utilisateur doit impérativement être membre du groupe (`security_service.LeftMember`) pour solliciter des suggestions d'ajouts.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `conversation_id`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Contrôle sécuritaire de la présence de l'utilisateur dans la conversation. (Renvoie 403 Forbidden si l'utilisateur y est étranger).
// @Description  2. Génération d'une liste de recommandations (amis mutuels, relations proches non bannies).
// @Description  3. Filtration des entités déjà membres.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `conversation_models.SuggestOutput` contenant une liste de profils.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description  - **[INVALID_PAYLOAD] Paramètres invalides:**
// @Description    - **Trigger:** Le payload JSON de la requête est absent, mal formaté ou ne respecte pas les contraintes de validation du modèle attendu.
// @Description    - **Execution stage:** Phase de binding GIN et validation initiale des entrées, avant l'exécution de la logique métier.
// @Description    - **Response:** `numan_error.PublicErrorResponse` contenant le message d'erreur.
// @Description    - **Error code:** `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description  - **[UNAUTHORIZED]**
// @Description    - **Trigger:** Le token JWT est absent de l'en-tête Authorization, mal formaté, expiré ou invalide.
// @Description    - **Execution stage:** Middleware d'authentification JWT, avant que la requête n'atteigne le handler.
// @Description    - **Response:** `numan_error.PublicErrorResponse` contenant le message d'erreur.
// @Description    - **Error code:** `numan_error.CodeUnauthorized` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🔴 **403 Forbidden:**
// @Description  - **[FORBIDDEN_ACCESS] Accès refusé:**
// @Description    - **Trigger:** Vous ne faites pas partie de ce groupe, la fonctionnalité de suggestion vous est fermée.
// @Description    - **Execution stage:** Évaluation des permissions métier dans le handler ou le service appelé, après authentification.
// @Description    - **Response:** `numan_error.PublicErrorResponse` contenant le message d'erreur.
// @Description    - **Error code:** `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         conversations
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   conversation_models.SuggestInput true "Payload relatif à la liste de suggestions"
// @Success      200  {object}  conversation_models.SuggestOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Router       /conversation/suggest [post]
func SuggestContactsHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input conversation_models.SuggestInput
	// Binding strict sur le JSON
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le JSON de la requête est mal formaté.", err))
		return
	}

	output, errService := conversation_service.SuggestContacts(c.Request.Context(), callerID, input)
	if errService != nil {
		numan_error.RespondWithError(c, errService)
		return
	}

	c.JSON(http.StatusOK, output)
}
