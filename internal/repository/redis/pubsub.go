package redis

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// Subscription encapsule un abonnement Redis Pub/Sub (Standalone/Sentinel) ou Sharded Pub/Sub (Redis Cluster 7)
// sans exposer *go-redis.PubSub aux appelants externes.
type Subscription struct {
	pubsub    *goredis.PubSub
	sharded   bool
	startOnce sync.Once
	closeOnce sync.Once
	msgCh     chan PubSubMessage
	closedCh  chan struct{}
}

// NewPubSubSubscription ouvre une session d'abonnement Pub/Sub sur le Tier 3 Redis Pub/Sub.
// Retourne nil si aucun client Pub/Sub n'est initialisé.
func NewPubSubSubscription(ctx context.Context) *Subscription {
	psClient := redisgo.PubSubClient()
	if psClient == nil {
		return nil
	}
	isCluster := redisgo.IsClusterClient(psClient)
	var ps *goredis.PubSub
	if isCluster {
		ps = psClient.SSubscribe(ctx)
	} else {
		ps = psClient.Subscribe(ctx)
	}
	return &Subscription{
		pubsub:   ps,
		sharded:  isCluster,
		msgCh:    make(chan PubSubMessage, 256),
		closedCh: make(chan struct{}),
	}
}

// Subscribe abonne dynamiquement la session à un ou plusieurs canaux (SSUBSCRIBE en Cluster, SUBSCRIBE sinon).
func (s *Subscription) Subscribe(ctx context.Context, channels ...string) error {
	if s == nil || s.pubsub == nil || len(channels) == 0 {
		return nil
	}
	if s.sharded {
		return s.pubsub.SSubscribe(ctx, channels...)
	}
	return s.pubsub.Subscribe(ctx, channels...)
}

// Unsubscribe désabonne dynamiquement la session d'un ou plusieurs canaux (SUNSUBSCRIBE en Cluster, UNSUBSCRIBE sinon).
func (s *Subscription) Unsubscribe(ctx context.Context, channels ...string) error {
	if s == nil || s.pubsub == nil || len(channels) == 0 {
		return nil
	}
	if s.sharded {
		return s.pubsub.SUnsubscribe(ctx, channels...)
	}
	return s.pubsub.Unsubscribe(ctx, channels...)
}

// Channel retourne un canal Go de messages typés (PubSubMessage) fermé automatiquement à la fermeture de l'abonnement.
func (s *Subscription) Channel() <-chan PubSubMessage {
	if s == nil || s.pubsub == nil {
		ch := make(chan PubSubMessage)
		close(ch)
		return ch
	}
	s.startOnce.Do(func() {
		rawCh := s.pubsub.Channel()
		go func() {
			defer close(s.msgCh)
			for {
				select {
				case <-s.closedCh:
					return
				case msg, ok := <-rawCh:
					if !ok {
						return
					}
					select {
					case s.msgCh <- PubSubMessage{
						Channel: msg.Channel,
						Payload: msg.Payload,
					}:
					case <-s.closedCh:
						return
					}
				}
			}
		}()
	})
	return s.msgCh
}

// Close ferme proprement la connexion Pub/Sub sous-jacente et libère la goroutine de relais.
func (s *Subscription) Close() error {
	if s == nil || s.pubsub == nil {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {
		close(s.closedCh)
		err = s.pubsub.Close()
	})
	return err
}

// PushFlux publie un message lourd (Pattern Claim Check : stockage RAM puis notification Pub/Sub).
func (c *Collection) PushFlux(ctx context.Context, message []byte) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	messageID := strconv.FormatInt(domain.NowTime().UnixNano(), 10)

	payloadKey := c.Key("msg:" + messageID)
	if err := c.client.Set(ctx, payloadKey, message, c.DefaultTTL).Err(); err != nil {
		return err
	}

	channelKey := c.Key("channel")
	if redisgo.IsClusterClient(c.client) {
		return c.client.SPublish(ctx, channelKey, messageID).Err()
	}
	return c.client.Publish(ctx, channelKey, messageID).Err()
}

// SubscribeFlux s'abonne au canal et récupère automatiquement les payloads (Claim Check).
func (c *Collection) SubscribeFlux(ctx context.Context) (<-chan []byte, context.CancelFunc) {
	ch := make(chan []byte, 100)
	subCtx, cancel := context.WithCancel(ctx)
	if !c.IsReady() {
		close(ch)
		return ch, cancel
	}

	channelKey := c.Key("channel")
	var pubsub *goredis.PubSub
	if redisgo.IsClusterClient(c.client) {
		pubsub = c.client.SSubscribe(subCtx, channelKey)
	} else {
		pubsub = c.client.Subscribe(subCtx, channelKey)
	}

	go func() {
		defer func() {
			if err := pubsub.Close(); err != nil {
				numan_log.Error(ctx).Err(err).Msg("Erreur fermeture pubsub")
			}
		}()
		defer close(ch)

		for msg := range pubsub.Channel() {
			messageID := msg.Payload
			payloadKey := c.Key("msg:" + messageID)

			readCtx, readCancel := context.WithTimeout(subCtx, 2*time.Second)
			data, err := c.client.Get(readCtx, payloadKey).Bytes()
			readCancel()

			if err != nil {
				continue
			}

			select {
			case ch <- data:
			case <-subCtx.Done():
				return
			}
		}
	}()

	return ch, cancel
}

// Publish publie un message sur un canal Pub/Sub (SPUBLISH en mode Redis Cluster 7).
func (c *Collection) Publish(ctx context.Context, id any, message any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if redisgo.IsClusterClient(c.client) {
		return c.client.SPublish(ctx, c.Key(id), message).Err()
	}
	return c.client.Publish(ctx, c.Key(id), message).Err()
}

// PublishMultiple publie un même message sur plusieurs canaux via un Pipeline borné (O(1) RTT).
// Utilise SPUBLISH en mode Redis Cluster 7 pour éviter tout broadcast inter-nœuds.
func (c *Collection) PublishMultiple(ctx context.Context, ids []int64, message any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	isCluster := redisgo.IsClusterClient(c.client)

	for start := 0; start < len(ids); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		pipe := c.client.Pipeline()
		for _, id := range chunk {
			if isCluster {
				pipe.SPublish(ctx, c.Key(id), message)
			} else {
				pipe.Publish(ctx, c.Key(id), message)
			}
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
