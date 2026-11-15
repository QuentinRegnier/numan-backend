package like_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/like_service"
	"github.com/gin-gonic/gin"

	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// GetPostLikesHandler godoc
// @Summary      Récupérer les abonnés ayant liké une publication
// @Description  Récupère la liste paginée et hydratée des abonnés qui ont liké un post spécifique.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT requis).
// @Description  - Required permissions or roles: Vérification de la visibilité du Post (Public, Abonnés, Amis) et du statut de blocage.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `post_id` (int64).
// @Description  - Optional fields: `limit` (int, par défaut 20, max 100), `offset` (int).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation** : Vérification du payload et récupération du requérant. Correction de la limite si elle dépasse 100 ou est négative.
// @Description  2. **Contrôle d'Accès au Post** : Chargement en cascade (L1 -> L2 -> L3) du Post demandé. Vérification stricte des permissions selon la relation (Abonné, Ami) et la visibilité définie par l'auteur (ou blocage par l'auteur).
// @Description  3. **Récupération des Likes** : Chargement des identifiants (Mongo L2 fallback vers Postgres L3 avec auto-guérison asynchrone).
// @Description  4. **Hydratation Rapide** : Chaque profil d'utilisateur ayant liké est hydraté via le Speed Cache (UserLite, Statut en Ligne) et son avatar est généré si applicable.
// @Description  5. **Réponse** : Renvoi de la liste sécurisée et hydratée.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `like_models.GetPostLikesOutput` contenant un tableau d'utilisateurs (`UserLiteView`).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request — Payload invalide**
// @Description  - **Trigger:** Le payload JSON de la requête est absent ou ne respecte pas le modèle.
// @Description  - **Execution stage:** Binding GIN.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeInvalidPayload` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  🟠 **401 Unauthorized — Authentification échouée**
// @Description  - **Trigger:** Le token JWT est absent, invalide ou expiré.
// @Description  - **Execution stage:** Middleware d'authentification ou fonction `pkg.GetUserIDFromContext`.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeUnauthorized` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  🔴 **403 Forbidden — Accès refusé**
// @Description  - **Trigger:** L'auteur du post a bloqué le requérant, ou la visibilité du post requiert une relation (Ami/Abonné) que le requérant ne possède pas.
// @Description  - **Execution stage:** Évaluation de la Matrice de Confidentialité dans le service.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeForbidden` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  ⚫ **404 Not Found — Post introuvable**
// @Description  - **Trigger:** Le Post spécifié n'existe pas ou son statut est "Supprimé" (`PostVisibilityDeleted`).
// @Description  - **Execution stage:** Phase de chargement en cascade du Post (L1 -> L2 -> L3) dans le service.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeNotFound` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  ⚫ **500 Internal Server Error — Erreur inattendue**
// @Description  - **Trigger:** Une erreur de connectivité s'est produite lors des requêtes vers PostgreSQL.
// @Description  - **Execution stage:** Requête à la base de données.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeInternalError` (renvoyé dans le champ JSON `code`).
// @Tags         likes
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   like_models.GetPostLikesInput true "Payload relatif à la liste des likes d'un post spécifique"
// @Success      200  {object}  like_models.GetPostLikesOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /like/post/get [post]
func GetPostLikesHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input like_models.GetPostLikesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	input.CallerID = callerID

	output, err := like_service.GetPostLikes(c.Request.Context(), input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, output)
}
