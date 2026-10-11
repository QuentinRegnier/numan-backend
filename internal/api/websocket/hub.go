package websocket

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/go-redis/redis/v8"
)

// broadcastMessage est l'enveloppe interne pour transférer l'écoute Redis vers la boucle locale
type broadcastMessage struct {
	ChannelType string // "user" ou "community"
	TargetID    int64
	Payload     []byte
}

type communitySubscription struct {
	Client      *client
	CommunityID int64
}

type hub struct {
	// Mode "Fan-out" (Multi-Device)
	Clients map[int64]map[*client]bool

	// Mode "Twitch" (Duplication RAM)
	Communities map[int64]map[*client]bool

	Register           chan *client
	Unregister         chan *client
	SubscribeCommunity chan communitySubscription

	// SEULE voie autorisée pour ordonner l'envoi d'un message (Anti Race-Condition)
	BroadcastRoute chan broadcastMessage

	PubSub *redis.PubSub
}

var globalHub *hub

func InitHub() {
	globalHub = &hub{
		Clients:            make(map[int64]map[*client]bool),
		Communities:        make(map[int64]map[*client]bool),
		Register:           make(chan *client),
		Unregister:         make(chan *client),
		SubscribeCommunity: make(chan communitySubscription),
		BroadcastRoute:     make(chan broadcastMessage, 2048), // Buffer haute capacité
		PubSub:             redisgo.Rdb.Subscribe(context.Background()),
	}

	go globalHub.run()
	go globalHub.listenRedis()
}

// listenRedis capte les événements du réseau global et les pousse dans le sas d'attente
func (h *hub) listenRedis() {
	ch := h.PubSub.Channel()
	for msg := range ch {
		parts := strings.Split(msg.Channel, ":")
		if len(parts) != 3 {
			continue
		}

		channelType := parts[1] // "user" ou "community"
		var targetID int64
		_, err := fmt.Sscanf(parts[2], "%d", &targetID)
		if err != nil {
			numan_log.Warn(context.Background()).Err(err).Str("channel", msg.Channel).Msg("Impossible de parser l'ID de la cible")
			continue
		}

		h.BroadcastRoute <- broadcastMessage{
			ChannelType: channelType,
			TargetID:    targetID,
			Payload:     []byte(msg.Payload),
		}
	}
}

// run est l'unique boucle autorisée à muter les maps et écrire dans client.Send
func (h *hub) run() {
	for {
		select {
		case c := <-h.Register:
			if _, ok := h.Clients[c.UserID]; !ok {
				h.Clients[c.UserID] = make(map[*client]bool)
				// Abonnement dynamique au cluster
				channel := fmt.Sprintf("channel:user:%d", c.UserID)
				err := h.PubSub.Subscribe(context.Background(), channel)
				if err != nil {
					numan_log.Warn(context.Background()).Err(err).Str("channel", channel).Msg("Impossible de s'abonner au canal")
				}
			}
			h.Clients[c.UserID][c] = true
		case c := <-h.Unregister:
			if connections, ok := h.Clients[c.UserID]; ok {
				if _, ok := connections[c]; ok {
					delete(connections, c)
					close(c.Send)

					if len(connections) == 0 {
						delete(h.Clients, c.UserID)
						// Désabonnement réseau pour économiser le CPU Redis
						channel := fmt.Sprintf("channel:user:%d", c.UserID)
						err := h.PubSub.Unsubscribe(context.Background(), channel)
						if err != nil {
							numan_log.Warn(context.Background()).Err(err).Str("channel", channel).Msg("Impossible de se désabonner du canal")
						}
					}
				}
			}

		case sub := <-h.SubscribeCommunity:
			if _, ok := h.Communities[sub.CommunityID]; !ok {
				h.Communities[sub.CommunityID] = make(map[*client]bool)
				channel := fmt.Sprintf("channel:community:%d", sub.CommunityID)
				err := h.PubSub.Subscribe(context.Background(), channel)
				if err != nil {
					numan_log.Warn(context.Background()).Err(err).Str("channel", channel).Msg("Impossible de s'abonner au canal de communauté")
				}
			}
			h.Communities[sub.CommunityID][sub.Client] = true

		case msg := <-h.BroadcastRoute:
			if msg.ChannelType == "user" {
				if connections, ok := h.Clients[msg.TargetID]; ok {
					for client := range connections {
						select {
						case client.Send <- msg.Payload:
						default:
							// Le client a un buffer plein (plantage réseau), on le coupe
							close(client.Send)
							delete(connections, client)
						}
					}
				}
			} else if msg.ChannelType == "community" {
				if connections, ok := h.Communities[msg.TargetID]; ok {
					for client := range connections {
						select {
						case client.Send <- msg.Payload:
						default:
							close(client.Send)
							delete(connections, client)
						}
					}
				}
			}
		}
	}
}
