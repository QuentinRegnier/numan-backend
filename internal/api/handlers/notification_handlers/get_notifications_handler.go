package notification_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/gin-gonic/gin"
)

// GetNotificationsHandler godoc
// @Summary      Lister les notifications
// @Description  Récupère la liste paginée des notifications de l'utilisateur avec un système de lecture en cascade (Cache ZSET L1 -> Mongo L2) et une auto-guérison.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée nécessitant un token JWT d'authentification valide.
// @Description  - Required permissions or roles: L'utilisateur appelant accède uniquement à sa propre boîte de notifications.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation Initiale :**
// @Description     - Identification de l'appelant via le token JWT du contexte.
// @Description     - Binding GIN du payload (`Limit`, `Offset`, `Force`).
// @Description  2. **Purge Optionnelle :**
// @Description     - Si le mode `Force` est activé, la RAM L1 (ZSET des notifications) est purgée pour forcer un rafraîchissement depuis L2.
// @Description  3. **Stratégie de Lecture en Cascade :**
// @Description     - **L1 (Redis ZSET) :** Tentative de récupération des IDs si l'offset est faible (< 100) et sans `Force`.
// @Description     - **L2 (MongoDB Fallback) :** En cas de dépassement d'offset ou Cache Miss, chargement paginé depuis Mongo. Une auto-guérison asynchrone réalimente le ZSET L1 et l'Object Cache en arrière-plan.
// @Description  4. **Hydratation et Construction :**
// @Description     - Si lecture depuis L1 : Hydratation via MGET sur l'Object Cache L1 (`object_cache_service.GetNotificationsView`).
// @Description     - Traitement métier avec la fonction interne `hydrateNotificationView` pour ajouter avatars signés, informations sur la cible et métadonnées annexes.
// @Description     - Formatage en `notification_models.GetNotificationsOutput` et réponse HTTP 200.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[numan_error.CodeInvalidPayload] Requête mal formatée ou paramètres invalides:**
// @Description    - Trigger: Format JSON invalide ou paramètres de pagination (limit/offset) erronés.
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
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[numan_error.CodeInternalError] Erreur de service inattendue:**
// @Description    - Trigger: Panne de la base MongoDB (L2) lors d'un Cache Miss, ou erreur d'accès massive à Redis (MGET).
// @Description    - Execution stage: Exécution profonde du service métier.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`
// @Tags         notifications
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   notification_models.GetNotificationsInput true "Payload pour récupérer la liste paginée des notifications de l'utilisateur"
// @Success      200  {object}  notification_models.GetNotificationsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /notifications/get [post]
func GetNotificationsHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input notification_models.GetNotificationsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	notifs, err := notification_service.GetNotifications(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	if notifs == nil {
		notifs = make([]notification_models.NotificationView, 0)
	}

	c.JSON(http.StatusOK, notification_models.GetNotificationsOutput{Notifications: notifs})
}
