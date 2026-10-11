package post_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/post_service"
	"github.com/gin-gonic/gin"
)

// GetPostHandler godoc
// @Summary      Récupérer un batch de publications
// @Description  Récupère un ensemble spécifique de posts par leurs identifiants en respectant rigoureusement les règles de visibilité et le statut relationnel entre l'appelant et les auteurs. Les accès illégitimes ne causent pas d'erreur 403/404 HTTP, mais incluent un message d'erreur structuré dans la liste retournée.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT d'authentification valide.
// @Description  - Required permissions or roles: Route sécurisée ; les données exposées dépendent du niveau relationnel avec l'auteur (Amis, Abonnés, Public, ou Bloqué).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction et Validation :**
// @Description     - Extraction de l'ID appelant via le contexte.
// @Description     - Binding GIN de la liste d'IDs (`PostIDs`). Déduplication et vérification de la limite stricte (Maximum 50 posts simultanés).
// @Description     - Arrêt anticipé avec tableau vide si la liste est vide.
// @Description  2. **Récupération Brut (Cascade) :**
// @Description     - Appel du service `post_service.GetPosts`, qui orchestre une lecture en cascade (Cache L1 -> Mongo L2 -> Postgres L3) via un Helper de résolution massive (`fetchPostsCascade`).
// @Description  3. **Matrice de Visibilité et Règles Relationnelles :**
// @Description     - Sur chaque post, vérification en temps réel (RAM O(1)) du statut relationnel (`cache_service.RelationValue`).
// @Description     - Application silencieuse des règles d'exclusion : Bannissement croisé (Shadow ban mutuel), Soft Delete, Exclusivité Abonnés ou Amis.
// @Description     - En cas d'exclusion, l'objet de réponse encapsule un message d'erreur explicatif au lieu du payload.
// @Description  4. **Hydratation des Payloads Valides :**
// @Description     - Pour les posts accessibles, récupération des données de l'auteur (`GetUserLite`).
// @Description     - Génération des URL cryptographiques HMAC pour l'avatar de l'auteur et les pièces jointes (`media_service.FormatMediaViewsCascade`).
// @Description     - Chargement synchrone du Top des commentaires associés (limité au ZSET L1 Cap).
// @Description  5. **Réponse :**
// @Description     - Renvoi HTTP 200 contenant le tableau `GetPostOutput`, regroupant les posts hydratés et les erreurs de visibilité encapsulées.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Requête mal formatée ou paramètres invalides:**
// @Description    - Trigger: Format JSON invalide ou dépassement de la limite stricte de 50 posts par requête.
// @Description    - Execution stage: Validation du payload et bouclier de protection BATCH dans le handler.
// @Description    - Response: `numan_error.PublicErrorResponse` avec détails, ou map JSON selon le format du routeur HTTP.
// @Description    - Error code: `numan_error.CodeInvalidPayload`
// @Description
// @Description  🟡 **401 Unauthorized:**
// @Description
// @Description  - **[numan_error.CodeUnauthorized] Jeton invalide ou absent:**
// @Description    - Trigger: L'utilisateur appelant n'est pas identifié (Token manquant ou illisible par pkg).
// @Description    - Execution stage: Extraction manuelle du contexte utilisateur via pkg.GetUserIDFromContext.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur de service inattendue:**
// @Description    - Trigger: Aucune erreur 500 n'est explicitement levée ici, le service de résolution massive absorbant les défaillances avec des structures non-trouvées.
// @Description    - Execution stage: Exécution profonde du service métier.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`
// @Tags         posts
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   post_models.GetPostInput true "Payload pour récupérer un batch de posts spécifiques (liste d'IDs, max 50)"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /posts/get [post]
func GetPostHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input post_models.GetPostInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou post_ids manquants.", err))
		return
	}

	// Appel du service hydraté (qui renvoie maintenant des GetPostOutput avec l'auteur)
	results, err := post_service.GetPosts(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	// Le routeur HTTP sert directement la structure DTO propre
	c.JSON(http.StatusOK, results)
}
