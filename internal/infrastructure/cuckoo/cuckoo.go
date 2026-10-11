package cuckoo

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	redisgo "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	cuckoo "github.com/seiflotfy/cuckoofilter"
)

// GlobalCuckoo est l'instance unique du filtre (Singleton)
var GlobalCuckoo *cuckoo.Filter

// Constantes pour le flux de synchro
const (
	CuckooChannel = "cuckoo-sync"
	ActionAdd     = "ADD"
	ActionDel     = "DEL"
)

// cuckooMessage définit le format des messages envoyés dans le Flux Redis
type cuckooMessage struct {
	Action string // "ADD" ou "DEL"
	Key    string // ex: "username:toto"
}

// InitCuckooFilter initialise la mémoire du filtre et lance l'écoute Redis.
// Le warm-up avec les données est désormais orchestré par la couche Service.
func InitCuckooFilter() {
	numan_log.Info(context.Background()).Msg("Initialisation du Cuckoo Filter en mémoire...")

	// 1. Création du filtre (Capacité 1M, peut être ajusté)
	GlobalCuckoo = cuckoo.NewFilter(1000000)

	// 2. Lancement de la synchro inter-serveurs (Flux Redis)
	go startCuckooSync()
}

// startCuckooSync écoute le flux Redis pour mettre à jour le filtre local
func startCuckooSync() {
	// Utilisation pure DDD : la Collection encapsule tout (canal, nom, client)
	msgChan, cancel := redisgo.CuckooSync.SubscribeFlux(context.Background())
	defer cancel()

	numan_log.Info(context.Background()).Msg("Cuckoo Sync : écoute du flux Redis activée.")

	for payload := range msgChan {
		var msg cuckooMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			numan_log.Error(context.Background()).Err(err).Msg("Erreur décodage message Cuckoo")
			continue
		}

		if msg.Action == ActionAdd {
			GlobalCuckoo.Insert([]byte(msg.Key))
		} else if msg.Action == ActionDel {
			GlobalCuckoo.Delete([]byte(msg.Key))
		}
	}
}

// BroadcastCuckooUpdate envoie un signal aux autres serveurs via Redis
func BroadcastCuckooUpdate(action, field, value string) {
	msg := cuckooMessage{
		Action: action,
		Key:    field + ":" + value,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("Erreur encodage message Cuckoo")
		return
	}

	if err := redisgo.CuckooSync.PushFlux(context.Background(), data); err != nil {
		numan_log.Error(context.Background()).Err(err).Msg("Erreur Broadcast Cuckoo")
	}
}
