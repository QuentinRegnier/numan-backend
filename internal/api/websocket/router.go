package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/api/websocket/ws_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// route intercepte le message brut, l'oriente et gère la réponse avec un bouclier Zero-Trust.
func (c *client) route(message []byte) {
	var req wsRequest
	if err := json.Unmarshal(message, &req); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("WS Route Error: Payload illisible")
		return
	}

	ctx := context.Background()

	// =========================================================================
	// BOUCLIER DE SÉCURITÉ ZERO-TRUST (HMAC & Anti-Rejeu)
	// =========================================================================

	// 1. Anti-Rejeu (Timestamp)
	tsInt, err := pkg.ParseInt64Strict(req.Timestamp)
	if err != nil {
		c.sendError(req.RequestID, "Timestamp invalide")
		return
	}
	now := time.Now().Unix()
	if math.Abs(float64(now-tsInt)) > float64(variables.ToleranceTimeSeconds) {
		c.sendError(req.RequestID, "Trame expirée (Anti-Rejeu)")
		return
	}

	// 2. Récupération instantanée de la session Ratchet en RAM (Speed Cache O(1))
	// Cela permet de supporter la rotation de clé HTTP sans couper le WebSocket !
	session, err := cache_service.LoadSessionFromCache(ctx, c.UserID, c.DeviceID, "")
	if err != nil || session.ID == 0 {
		c.sendError(req.RequestID, "Session invalide ou expirée")
		return
	}

	// 3. Validation de la signature HMAC (Action|Timestamp|Payload)
	stringToSign := fmt.Sprintf("%s|%s|%s", req.Action, req.Timestamp, string(req.Payload))

	isValid := security.CheckHMAC(stringToSign, session.CurrentSecret, req.Signature)

	// Tolérance de rotation de clé (Exactement comme en HTTP)
	if !isValid && session.LastSecret != "" && session.ToleranceTime > 0 && time.Now().Before(domain.MillisToTime(session.ToleranceTime)) {
		isValid = security.CheckHMAC(stringToSign, session.LastSecret, req.Signature)
	}

	if !isValid {
		c.sendError(req.RequestID, "Signature HMAC invalide")
		return
	}
	// =========================================================================

	var resData any
	var routeErr error

	// =========================================================================
	// Le Grand Switch (Remplace ton HTTP routes.go)
	switch req.Action {

	// --- NOUVEAU : GESTION DE LA PRÉSENCE (HEARTBEAT) ---
	case "ping":
		// 1. Prolonge le TTL de 90 secondes en O(1) dans le L1
		routeErr = cache_service.MarkUserOnline(ctx, c.UserID)
		// 2. On renvoie simplement un petit objet vide (ou juste le status "success" via resData)
		resData = map[string]string{"status": "pong"}
	// --- CAS A : SYNC PRÉSENCE EN LOT (INBOX "DOOM") ---
	case "presence.sync":
		resData, routeErr = ws_handlers.HandleSyncPresence(ctx, req.Payload)

	// --- CAS B : PRÉSENCE INSTANTANÉE EN CONVERSATION ACTIVE ---
	case "conversation.focus":
		routeErr = ws_handlers.HandleFocusConversation(ctx, c.UserID, req.Payload)
	// --- TYPING (Volatil) ---
	case "typing.started":
		routeErr = ws_handlers.HandleTyping(ctx, c.UserID, req.Payload, true)
	case "typing.stopped":
		routeErr = ws_handlers.HandleTyping(ctx, c.UserID, req.Payload, false)

	// --- CONVERSATIONS ---
	case "conversation.read":
		resData, routeErr = ws_handlers.HandleReadReceipt(ctx, c.UserID, req.Payload)

	// --- MESSAGES ---
	case "message.create":
		resData, routeErr = ws_handlers.HandleCreateMessage(ctx, c.UserID, req.Payload)
	case "message.update":
		resData, routeErr = ws_handlers.HandleUpdateMessage(ctx, c.UserID, req.Payload)
	case "message.delete":
		resData, routeErr = ws_handlers.HandleDeleteMessage(ctx, c.UserID, req.Payload)
	case "message.react":
		routeErr = ws_handlers.HandleReactMessage(ctx, c.UserID, req.Payload)
	case "message.unreact":
		routeErr = ws_handlers.HandleUnreactMessage(ctx, c.UserID, req.Payload)

	default:
		c.sendError(req.RequestID, "Action non reconnue")
		return
	}

	// Gestion de la réponse à renvoyer au client
	if routeErr != nil {
		c.sendError(req.RequestID, routeErr.Error())
	} else {
		c.sendSuccess(req.RequestID, resData)
	}
}

// Helpers pour standardiser les retours
func (c *client) sendSuccess(reqID string, data any) {
	resp := wsResponse{EventType: "response", RequestID: reqID, Status: "success", Data: data}
	b, err := json.Marshal(resp)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("WS Marshal Error: Impossible de sérialiser la réponse")
		return
	}
	c.Send <- b
}

func (c *client) sendError(reqID string, errStr string) {
	resp := wsResponse{EventType: "response", RequestID: reqID, Status: "error", Error: errStr}
	b, err := json.Marshal(resp)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("WS Marshal Error: Impossible de sérialiser l'erreur")
		return
	}
	c.Send <- b
}
