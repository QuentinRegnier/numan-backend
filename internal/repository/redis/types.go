package redis

// ZMember représente une entrée (Score, Member) d'un Sorted Set Redis sans exposer go-redis.Z.
type ZMember struct {
	Score  float64
	Member any
}

// GetManyResult regroupe les payloads trouvés (bruts MsgPack/bytes) et les IDs manquants d'une lecture batch.
type GetManyResult struct {
	Found      map[int64][]byte
	MissingIDs []int64
}

// ShardMemorySnapshot capture l'état mémoire et client d'un nœud/shard Redis côté repository (sans fuiter internal/infrastructure/redis).
type ShardMemorySnapshot struct {
	ShardID          string  `json:"shard_id"`
	Role             string  `json:"role"`
	NodeHostname     string  `json:"node_hostname"`
	UsedBytes        int64   `json:"used_bytes"`
	MaxBytes         int64   `json:"max_bytes"`
	FillRatio        float64 `json:"fill_ratio"`
	EvictedKeys      int64   `json:"evicted_keys"`
	ConnectedClients int64   `json:"connected_clients"`
}

// TierTelemetrySnapshot agrège les statistiques temps réel d'un tier Redis côté repository.
type TierTelemetrySnapshot struct {
	Tier             string                `json:"tier"`
	Mode             string                `json:"mode"`
	Dedicated        bool                  `json:"dedicated"`
	Shards           []ShardMemorySnapshot `json:"shards"`
	TotalUsedBytes   int64                 `json:"total_used_bytes"`
	TotalMaxBytes    int64                 `json:"total_max_bytes"`
	TotalEvictedKeys int64                 `json:"total_evicted_keys"`
	PubSubChannels   int64                 `json:"pubsub_channels,omitempty"`
}

// PubSubMessage représente un message reçu depuis un canal Redis Pub/Sub ou Sharded Pub/Sub.
type PubSubMessage struct {
	Channel string
	Payload string
}
