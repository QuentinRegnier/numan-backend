package profile_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/profile_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/profile_service"
	"github.com/gin-gonic/gin"
)

// GetProfileHandler godoc
// @Summary      Récupérer un profil utilisateur
// @Description  Agrège toutes les informations publiques ou privées d'un profil (données de compte, paramètres de confidentialité, avatars, posts et relations) selon les règles de visibilité configurées.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT d'authentification valide.
// @Description  - Required permissions or roles: Accès autorisé pour tout utilisateur authentifié. La quantité de données exposées dépend du niveau de relation (Bloqué, Ami, Follower) et des paramètres de confidentialité de la cible.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation et Traitement Préalable :**
// @Description     - Binding GIN du paramètre optionnel `TargetID` (sinon, c'est le propre profil de l'appelant).
// @Description     - Initialisation des paramètres par défaut (`Limit` à 20).
// @Description  2. **Évaluation de la Matrice Relationnelle :**
// @Description     - Récupération en O(1) depuis le cache des relations bilatérales.
// @Description     - Si la cible a bloqué le requérant, la requête est immédiatement interrompue avec un retour HTTP 404 simulé (Shadow ban).
// @Description  3. **Contrôle Stricte de la Vie Privée :**
// @Description     - Récupération des réglages de confidentialité (L1->L2->L3).
// @Description     - Vérification de la propriété `ProfileVisibility` vis-à-vis du statut relationnel réel de l'appelant. Rejet avec HTTP 403 si l'accès est refusé, épargnant ainsi les requêtes à la BDD.
// @Description  4. **Hydratation Progressive (Cascade et Hub Métier) :**
// @Description     - **Identité** : Lecture L2/L3 de l'utilisateur. Promotion BDD asynchrone si trouvé en L3. Masquage de la géolocalisation ou statut en ligne si les réglages l'exigent.
// @Description     - **Médias** : Génération de l'avatar HMAC signé.
// @Description     - **Conversation Directe** : Vérification de l'existence d'un MP via Cache puis Postgres.
// @Description     - **Contenu** : Chargement du batch de posts via `post_service.GetUserPosts`.
// @Description     - **Interactions** : Hydratation des informations de Like et Sauvegardes de l'appelant sur les posts retournés.
// @Description  5. **Assemblage et Réponse :**
// @Description     - Sérialisation et retour de la structure composite `GetProfileOutput`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Requête mal formatée ou paramètres invalides:**
// @Description    - Trigger: Le payload est mal formaté.
// @Description    - Execution stage: Validation GIN.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`
// @Description
// @Description  🟡 **401 Unauthorized:**
// @Description
// @Description  - **[numan_error.CodeUnauthorized] Jeton invalide ou absent:**
// @Description    - Trigger: Le client HTTP n'envoie pas de token, ou le JWT a expiré.
// @Description    - Execution stage: Middleware global d'authentification.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`
// @Description
// @Description  🟠 **403 Forbidden:**
// @Description
// @Description  - **[numan_error.CodeForbidden] Accès refusé:**
// @Description    - Trigger: Le profil ciblé est réglé en mode privé et le niveau relationnel de l'appelant est insuffisant pour contourner cette restriction.
// @Description    - Execution stage: Évaluation de la règle de visibilité dans l'étape de contrôle strict de la vie privée.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`
// @Description
// @Description  🟤 **404 Not Found:**
// @Description
// @Description  - **[numan_error.CodeNotFound] Ressource introuvable:**
// @Description    - Trigger: L'utilisateur ciblé n'existe pas ou l'appelant a été bloqué (Shadow ban).
// @Description    - Execution stage: Vérification de la matrice relationnelle ou accès ultime en L3.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur de service inattendue:**
// @Description    - Trigger: Défaillance de la base de données principale (Postgres L3) lors du fallback de chargement de l'utilisateur.
// @Description    - Execution stage: Exécution profonde du service métier.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`
// @Tags         profile
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   profile_models.GetProfileInput true "Payload pour récupérer le profil d'un utilisateur spécifique (optionnel, sinon le profil de l'appelant est retourné)"
// @Success      200  {object}  profile_models.GetProfileOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /profile/get [post]
func GetProfileHandler(c *gin.Context) {
	// 1. Identification de l'appelant
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	// 2. Parsing du payload JSON
	var input profile_models.GetProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de la requête invalide.", err))
		return
	}

	// 3. Appel du Service Hub
	output, err := profile_service.GetProfile(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	// 4. Réponse
	c.JSON(http.StatusOK, output)
}
