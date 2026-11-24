package websocket

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/redis/go-redis/v9"
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

	PubSub        *redis.PubSub
	shardedPubSub bool

	stopCh       chan struct{}
	doneCh       chan struct{}
	shutdownOnce sync.Once
	activeUsers  atomic.Int64
	activeConns  atomic.Int64
}

var (
	globalHub   *hub
	globalHubMu sync.RWMutex
)

func InitHub() {
	psClient := redisgo.PubSubClient()
	var ps *redis.PubSub
	isCluster := redisgo.IsClusterClient(psClient)
	if psClient != nil {
		if isCluster {
			ps = psClient.SSubscribe(context.Background())
		} else {
			ps = psClient.Subscribe(context.Background())
		}
	}

	h := &hub{
		Clients:            make(map[int64]map[*client]bool),
		Communities:        make(map[int64]map[*client]bool),
		Register:           make(chan *client),
		Unregister:         make(chan *client),
		SubscribeCommunity: make(chan communitySubscription),
		BroadcastRoute:     make(chan broadcastMessage, 2048), // Buffer haute capacité
		PubSub:             ps,
		shardedPubSub:      isCluster,
		stopCh:             make(chan struct{}),
		doneCh:             make(chan struct{}),
	}

	globalHubMu.Lock()
	globalHub = h
	globalHubMu.Unlock()

	go h.run()
	go h.listenRedis()
}

func (h *hub) subscribeChannel(ctx context.Context, channel string) error {
	if h.PubSub == nil {
		return nil
	}
	if h.shardedPubSub {
		return h.PubSub.SSubscribe(ctx, channel)
	}
	return h.PubSub.Subscribe(ctx, channel)
}

func (h *hub) unsubscribeChannel(ctx context.Context, channel string) error {
	if h.PubSub == nil {
		return nil
	}
	if h.shardedPubSub {
		return h.PubSub.SUnsubscribe(ctx, channel)
	}
	return h.PubSub.Unsubscribe(ctx, channel)
}

// Shutdown ferme proprement le Hub WebSocket, déconnecte les clients actifs et libère Redis PubSub.
func Shutdown() {
	globalHubMu.RLock()
	h := globalHub
	globalHubMu.RUnlock()
	if h == nil {
		return
	}
	h.shutdown()
}

func (h *hub) shutdown() {
	h.shutdownOnce.Do(func() {
		if h.PubSub != nil {
			_ = h.PubSub.Close()
		}
		close(h.stopCh)
		select {
		case <-h.doneCh:
		case <-time.After(3 * time.Second):
		}
	})
}

// HubStats retourne le nombre d'utilisateurs uniques et de connexions WebSocket actives (thread-safe).
func HubStats() (activeUsers int64, activeConnections int64) {
	globalHubMu.RLock()
	h := globalHub
	globalHubMu.RUnlock()
	if h == nil {
		return 0, 0
	}
	return h.activeUsers.Load(), h.activeConns.Load()
}

// listenRedis capte les événements du réseau global et les pousse dans le sas d'attente
func (h *hub) listenRedis() {
	if h.PubSub == nil {
		return
	}
	ch := h.PubSub.Channel()
	for {
		select {
		case <-h.stopCh:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
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

			select {
			case h.BroadcastRoute <- broadcastMessage{
				ChannelType: channelType,
				TargetID:    targetID,
				Payload:     []byte(msg.Payload),
			}:
			case <-h.stopCh:
				return
			}
		}
	}
}

func (h *hub) updateStats() {
	users := int64(len(h.Clients))
	var conns int64
	for _, set := range h.Clients {
		conns += int64(len(set))
	}
	h.activeUsers.Store(users)
	h.activeConns.Store(conns)
}

// run est l'unique boucle autorisée à muter les maps et écrire dans client.Send
func (h *hub) run() {
	defer close(h.doneCh)
	for {
		select {
		case <-h.stopCh:
			for userID, connections := range h.Clients {
				for c := range connections {
					close(c.Send)
					delete(connections, c)
				}
				delete(h.Clients, userID)
			}
			for commID := range h.Communities {
				delete(h.Communities, commID)
			}
			h.updateStats()
			return

		case c := <-h.Register:
			if _, ok := h.Clients[c.UserID]; !ok {
				h.Clients[c.UserID] = make(map[*client]bool)
				if h.PubSub != nil {
					// Abonnement dynamique au cluster (SSUBSCRIBE en Cluster, SUBSCRIBE en Standalone)
					channel := fmt.Sprintf("channel:user:%d", c.UserID)
					if err := h.subscribeChannel(context.Background(), channel); err != nil {
						numan_log.Warn(context.Background()).Err(err).Str("channel", channel).Msg("Impossible de s'abonner au canal")
					}
				}
			}
			h.Clients[c.UserID][c] = true
			h.updateStats()

		case c := <-h.Unregister:
			if connections, ok := h.Clients[c.UserID]; ok {
				if _, ok := connections[c]; ok {
					delete(connections, c)
					close(c.Send)

					if len(connections) == 0 {
						delete(h.Clients, c.UserID)
						if h.PubSub != nil {
							channel := fmt.Sprintf("channel:user:%d", c.UserID)
							if err := h.unsubscribeChannel(context.Background(), channel); err != nil {
								numan_log.Warn(context.Background()).Err(err).Str("channel", channel).Msg("Impossible de se désabonner du canal")
							}
						}
					}
				}
			}
			h.updateStats()

		case sub := <-h.SubscribeCommunity:
			if _, ok := h.Communities[sub.CommunityID]; !ok {
				h.Communities[sub.CommunityID] = make(map[*client]bool)
				if h.PubSub != nil {
					channel := fmt.Sprintf("channel:community:%d", sub.CommunityID)
					if err := h.subscribeChannel(context.Background(), channel); err != nil {
						numan_log.Warn(context.Background()).Err(err).Str("channel", channel).Msg("Impossible de s'abonner au canal de communauté")
					}
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
					if len(connections) == 0 {
						delete(h.Clients, msg.TargetID)
					}
					h.updateStats()
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
