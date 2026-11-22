package websocket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHub_ShutdownAndStats(t *testing.T) {
	prevHub := globalHub
	t.Cleanup(func() {
		globalHubMu.Lock()
		globalHub = prevHub
		globalHubMu.Unlock()
	})

	h := &hub{
		Clients:            make(map[int64]map[*client]bool),
		Communities:        make(map[int64]map[*client]bool),
		Register:           make(chan *client),
		Unregister:         make(chan *client),
		SubscribeCommunity: make(chan communitySubscription),
		BroadcastRoute:     make(chan broadcastMessage, 16),
		stopCh:             make(chan struct{}),
		doneCh:             make(chan struct{}),
	}

	globalHubMu.Lock()
	globalHub = h
	globalHubMu.Unlock()

	go h.run()

	c1 := &client{UserID: 42, Send: make(chan []byte, 4)}
	c2 := &client{UserID: 42, Send: make(chan []byte, 4)}
	c3 := &client{UserID: 99, Send: make(chan []byte, 4)}

	h.Register <- c1
	h.Register <- c2
	h.Register <- c3

	assert.Eventually(t, func() bool {
		users, conns := HubStats()
		return users == 2 && conns == 3
	}, time.Second, 10*time.Millisecond)

	// Shutdown doit être idempotent, fermer tous les canaux clients et remettre les compteurs à 0
	Shutdown()
	Shutdown()

	users, conns := HubStats()
	assert.Equal(t, int64(0), users)
	assert.Equal(t, int64(0), conns)

	_, open1 := <-c1.Send
	_, open2 := <-c2.Send
	_, open3 := <-c3.Send
	assert.False(t, open1)
	assert.False(t, open2)
	assert.False(t, open3)
}
