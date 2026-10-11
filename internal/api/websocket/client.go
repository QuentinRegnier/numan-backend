package websocket

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 1024
)

type client struct {
	Hub      *hub
	Conn     *websocket.Conn
	UserID   int64
	DeviceID string // NOUVEAU : Essentiel pour cibler la bonne session Ratchet en RAM
	Send     chan []byte
}

// readPump écoute les événements entrants du client (PONG et événements temps réel)
func (c *client) readPump() {
	defer func() {
		c.Hub.Unregister <- c
		err := c.Conn.Close()
		if err != nil {
			numan_log.Error(context.Background()).Err(err).Msg("WS readPump: Erreur lors de la fermeture de la connexion")
		}
		// Pas besoin d'appeler de fonction pour dire "offline" !
		// Si le client crashe ou quitte, les pings JSON s'arrêtent,
		// et Redis fera expirer le TTL de 90s tout seul. Magique.
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	err := c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("WS readPump: Erreur lors de la définition du ReadDeadline")
		return
	}

	// Optionnel : Tu peux garder le SetPongHandler si tu veux supporter les deux méthodes
	// (Pings natifs ET Pings JSON). C'est ce que je ferais en tant que Senior pour être ultra robuste.
	c.Conn.SetPongHandler(func(string) error {
		err = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			numan_log.Error(context.Background()).Err(err).Msg("WS readPump: Erreur lors de la mise à jour du ReadDeadline après PONG")
			return err
		}
		// Si un client envoie un vrai ping natif, on le marque aussi en ligne
		err = cache_service.MarkUserOnline(context.Background(), c.UserID)
		if err != nil {
			numan_log.Warn(context.Background()).Err(err).Int64("user_id", c.UserID).Msg("WS readPump: Impossible de marquer l'utilisateur comme en ligne après PONG")
		}
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			break // Déconnexion naturelle ou Timeout
		}

		// Dès qu'on reçoit un message (n'importe lequel : typing, message.create, ou ping),
		// on prolonge le bail de présence. L'activité prouve la présence.
		err = cache_service.MarkUserOnline(context.Background(), c.UserID)
		if err != nil {
			numan_log.Warn(context.Background()).Err(err).Int64("user_id", c.UserID).Msg("WS readPump: Impossible de marquer l'utilisateur comme en ligne après réception d'un message")
		}

		// On envoie le message brut au routeur !
		c.route(message)
	}
}

// handleTyping vérifie la sécurité et broadcast l'événement de frappe en O(1)
func (c *client) handleTyping(eventType string, convID int64) {
	ctx := context.Background()

	// SÉCURITÉ ZERO-TRUST : Le client WebSocket est peut-être malveillant.
	// On s'assure qu'il est bien membre de la conversation qu'il prétend cibler.
	// Cette requête est ultra-rapide (L1 Speed Cache Object)[cite: 16].
	mem, err := security_service.LeftMember(ctx, convID, c.UserID)
	if err != nil || mem.Role < 0 {
		return // Accès refusé, l'utilisateur a été banni ou n'est pas membre. On drop silencieusement.
	}

	// Préparation du payload à redistribuer aux autres membres
	broadcastPayload := map[string]any{
		"conversation_id": convID,
		"user_id":         c.UserID,
	}

	// Délégation au Pub/Sub : Cela va propager l'info dans les canaux Redis des autres utilisateurs[cite: 16].
	err = realtime_service.BroadcastToConversation(ctx, convID, eventType, broadcastPayload)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("conversation_id", convID).Int64("user_id", c.UserID).Msg("WS handleTyping: Impossible de broadcast l'événement de frappe")
		return
	}
}

// writePump est la seule goroutine qui écrit physiquement sur la connexion TCP
func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		err := c.Conn.Close()
		if err != nil {
			numan_log.Error(context.Background()).Err(err).Msg("WS writePump: Impossible de fermer la connexion")
		}
	}()

	for {
		select {
		case message, ok := <-c.Send:
			err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err != nil {
				numan_log.Error(context.Background()).Err(err).Msg("WS writePump: Impossible de définir le WriteDeadline")
				return
			}
			if !ok {
				err := c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				if err != nil {
					numan_log.Error(context.Background()).Err(err).Msg("WS writePump: Impossible d'écrire le message de fermeture")
				}
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, err = w.Write(message)
			if err != nil {
				numan_log.Error(context.Background()).Err(err).Msg("WS writePump: Impossible d'écrire le message")
				return
			}

			n := len(c.Send)
			for i := 0; i < n; i++ {
				_, err = w.Write(<-c.Send)
				if err != nil {
					numan_log.Error(context.Background()).Err(err).Msg("WS writePump: Impossible d'écrire le message")
					return
				}
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err != nil {
				numan_log.Error(context.Background()).Err(err).Msg("WS writePump: Impossible de définir le WriteDeadline")
				return
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
