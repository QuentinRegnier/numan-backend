package websocket

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // À affiner selon ta config CORS plus tard
	},
}

// ServeWS godoc
// @Summary      Connexion WebSocket
// @Description  Upgrade la requête HTTP entrante en une connexion WebSocket bidirectionnelle pour la réception d'événements en temps réel.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Nécessite un JWT valide passé par le JWTMiddleware.
// @Description  - Required permissions or roles: None
// @Description  - Relevant middleware: JWTMiddleware, RateLimiter, CORS
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: None (WebSocket Upgrade Request).
// @Description  - Optional fields: None.
// @Description  - Validation rules: Le JWT doit contenir un `sub` (UserID) et un `dev` (FirebaseInstallationID).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'UserID et du DeviceID depuis le contexte Gin (injectés par le middleware).
// @Description  2. Upgrade de la connexion HTTP en WebSocket via `gorilla/websocket`.
// @Description  3. Création du client WebSocket et enregistrement dans le `globalHub`.
// @Description  4. Marquage de l'utilisateur comme en ligne dans le cache (`cache_service.MarkUserOnline`).
// @Description  5. Lancement des goroutines `writePump` et `readPump`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 101 Switching Protocols
// @Description  - Response body: None (WebSocket stream)
// @Description  - Persistence guarantees: Mise à jour du cache en ligne de l'utilisateur.
// @Description  - Side effects: Enregistre la connexion dans le Hub, démarre les pompes de lecture/écriture.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request / 500 Internal Server Error:**
// @Description  - **[WEBSOCKET_UPGRADE_FAILED]**
// @Description    - Trigger: Échec lors du \`upgrader.Upgrade\`. Géré directement par Gorilla WebSocket.
// @Description    - Execution stage: Upgrade HTTP.
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description  - **[UNAUTHORIZED] Non autorisé:**
// @Description    - Trigger: Le middleware n'a pas injecté l'UserID ou sa récupération a échoué.
// @Description    - Execution stage: Récupération \`pkg.GetUserIDFromContext\`.
// @Description    - Response: \`numan_error.PublicErrorResponse\`.
// @Description
// @Description  - **[MISSING_DEVICE_ID] Device ID introuvable:**
// @Description    - Trigger: Le middleware n'a pas injecté \`firebaseInstallationID\`.
// @Description    - Execution stage: Lecture contexte Gin.
// @Description    - Response: \`numan_error.PublicErrorResponse\`.
// @Tags         websocket
// @Accept       json
// @Produce      json
// @Success      101 "Switching Protocols (WebSocket)"
// @Failure      401 {object} numan_error.PublicErrorResponse "Token invalide ou Device ID manquant"
// @Router       /ws [get]
func ServeWS(c *gin.Context) {
	// L'identité est extraite du JWT validé par le Middleware
	userID, err := pkg.GetUserIDFromContext(c)
	if err != nil || userID == 0 {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized("UNAUTHORIZED", "Non autorisé.", err))
		return
	}

	// NOUVEAU : Récupération du Device ID injecté par le JWTMiddleware
	deviceIDRaw, exists := c.Get("firebaseInstallationID")
	if !exists {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized("MISSING_DEVICE_ID", "Device ID introuvable dans le contexte.", nil))
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // L'erreur est gérée par upgrader et renvoyée au client via HTTP
	}

	client := &client{
		Hub:      globalHub,
		Conn:     conn,
		UserID:   userID,
		DeviceID: deviceIDRaw.(string),
		Send:     make(chan []byte, 256),
	}

	client.Hub.Register <- client
	err = cache_service.MarkUserOnline(context.Background(), userID)
	if err != nil {
		numan_log.Warn(context.Background()).Err(err).Int64("user_id", userID).Msg("Impossible de marquer l'utilisateur comme en ligne")
	}

	go client.writePump()
	go client.readPump()
}
