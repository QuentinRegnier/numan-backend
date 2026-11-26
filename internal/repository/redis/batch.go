package redis

import (
	"context"
	"errors"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

const defaultBatchChunkSize = 500

// mgetKeysClusterSafe exécute un MGET natif chunké sur Standalone/Sentinel ou un Pipeline de GET en mode Cluster.
//
// Sémantique batch :
//   - Entrée vide : retourne nil, nil sans appel réseau.
//   - Clés dupliquées : préserve la correspondance positionnelle 1:1 avec `keys`.
//   - Clés absentes : renvoie nil à l'index correspondant.
//   - Erreur partielle : retourne immédiatement la première erreur non-Nil rencontrée.
//   - Annulation de contexte : vérifiée avant chaque lot de taille bornée (defaultBatchChunkSize).
func mgetKeysClusterSafe(ctx context.Context, client goredis.UniversalClient, keys []string) ([]interface{}, error) {
	if client == nil {
		return nil, ErrNotInitialized
	}
	if len(keys) == 0 {
		return nil, nil
	}

	isCluster := redisgo.IsClusterClient(client)
	values := make([]interface{}, len(keys))

	for start := 0; start < len(keys); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[start:end]

		if !isCluster || len(chunk) == 1 {
			chunkVals, err := client.MGet(ctx, chunk...).Result()
			if err != nil {
				return nil, err
			}
			copy(values[start:end], chunkVals)
			continue
		}

		pipe := client.Pipeline()
		cmds := make([]*goredis.StringCmd, len(chunk))
		for i, k := range chunk {
			cmds[i] = pipe.Get(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for i, cmd := range cmds {
			val, err := cmd.Result()
			if err != nil {
				if errors.Is(err, goredis.Nil) {
					values[start+i] = nil
					continue
				}
				return nil, err
			}
			values[start+i] = val
		}
	}

	return values, nil
}

// deduplicateStrings préserve l'ordre de première apparition et ignore les clés vides.
func deduplicateStrings(keys []string) []string {
	if len(keys) <= 1 {
		if len(keys) == 1 && keys[0] == "" {
			return nil
		}
		return keys
	}
	seen := make(map[string]struct{}, len(keys))
	unique := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		unique = append(unique, k)
	}
	return unique
}

// delKeysClusterSafe exécute DEL natif chunké sur Standalone/Sentinel ou un Pipeline de DEL unitaires en mode Cluster.
// Déduplique les clés, respecte l'annulation du contexte et remonte toute erreur de commande.
func delKeysClusterSafe(ctx context.Context, client goredis.UniversalClient, keys []string) error {
	if client == nil {
		return nil
	}
	uniqueKeys := deduplicateStrings(keys)
	if len(uniqueKeys) == 0 {
		return nil
	}

	isCluster := redisgo.IsClusterClient(client)
	for start := 0; start < len(uniqueKeys); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(uniqueKeys) {
			end = len(uniqueKeys)
		}
		chunk := uniqueKeys[start:end]

		if !isCluster || len(chunk) == 1 {
			if err := client.Del(ctx, chunk...).Err(); err != nil {
				return err
			}
			continue
		}

		pipe := client.Pipeline()
		cmds := make([]*goredis.IntCmd, len(chunk))
		for i, k := range chunk {
			cmds[i] = pipe.Del(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		for _, cmd := range cmds {
			if err := cmd.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// unlinkKeysClusterSafe exécute UNLINK non-bloquant (chunké) sur Standalone/Sentinel ou un Pipeline de UNLINK unitaires en mode Cluster.
func unlinkKeysClusterSafe(ctx context.Context, client goredis.UniversalClient, keys []string) error {
	if client == nil {
		return nil
	}
	uniqueKeys := deduplicateStrings(keys)
	if len(uniqueKeys) == 0 {
		return nil
	}

	isCluster := redisgo.IsClusterClient(client)
	for start := 0; start < len(uniqueKeys); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(uniqueKeys) {
			end = len(uniqueKeys)
		}
		chunk := uniqueKeys[start:end]

		if !isCluster || len(chunk) == 1 {
			if err := client.Unlink(ctx, chunk...).Err(); err != nil {
				return err
			}
			continue
		}

		pipe := client.Pipeline()
		cmds := make([]*goredis.IntCmd, len(chunk))
		for i, k := range chunk {
			cmds[i] = pipe.Unlink(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		for _, cmd := range cmds {
			if err := cmd.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// MGet lit un lot de clés de la collection dans l'ordre exact des `ids` fournis (Cluster-safe).
func (c *Collection) MGet(ctx context.Context, ids ...any) ([]interface{}, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return mgetKeysClusterSafe(ctx, c.client, keys)
}

// GetMany récupère un lot d'objets MsgPack (Cluster-safe, chunké).
// Les IDs dupliqués sont dédupliqués sans erreur ; l'ordre de MissingIDs suit la première apparition dans `ids`.
func (c *Collection) GetMany(ctx context.Context, ids []int64) (*GetManyResult, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &GetManyResult{
			Found:      make(map[int64][]byte),
			MissingIDs: make([]int64, 0),
		}, nil
	}

	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}

	keys := make([]string, len(uniqueIDs))
	for i, id := range uniqueIDs {
		keys[i] = c.Key(id)
	}

	values, err := mgetKeysClusterSafe(ctx, c.client, keys)
	if err != nil {
		return nil, err
	}

	result := &GetManyResult{
		Found:      make(map[int64][]byte, len(uniqueIDs)),
		MissingIDs: make([]int64, 0),
	}

	for i, val := range values {
		originalID := uniqueIDs[i]
		if val == nil {
			result.MissingIDs = append(result.MissingIDs, originalID)
			continue
		}
		switch v := val.(type) {
		case string:
			result.Found[originalID] = []byte(v)
		case []byte:
			result.Found[originalID] = v
		default:
			result.MissingIDs = append(result.MissingIDs, originalID)
		}
	}
	return result, nil
}

// SetMany enregistre un lot d'objets en MsgPack dans Redis par lots bornés via Pipeline (Cluster-safe).
// Sérialise tout avant d'écrire afin d'éviter une écriture partielle sur erreur de marshaling.
func (c *Collection) SetMany(ctx context.Context, items map[int64]any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	type encodedItem struct {
		key  string
		data []byte
	}
	encoded := make([]encodedItem, 0, len(items))
	for id, data := range items {
		msgpackBytes, err := msgpack.Marshal(data)
		if err != nil {
			return err
		}
		encoded = append(encoded, encodedItem{
			key:  c.Key(id),
			data: msgpackBytes,
		})
	}

	for start := 0; start < len(encoded); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + defaultBatchChunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[start:end]

		pipe := c.client.Pipeline()
		cmds := make([]*goredis.StatusCmd, len(chunk))
		for i, item := range chunk {
			cmds[i] = pipe.Set(ctx, item.key, item.data, c.DefaultTTL)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		for _, cmd := range cmds {
			if err := cmd.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteMany supprime un lot d'identifiants int64 de la collection (Cluster-safe, chunké).
func (c *Collection) DeleteMany(ctx context.Context, ids []int64) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return delKeysClusterSafe(ctx, c.client, keys)
}

// UnlinkMany supprime de manière non-bloquante (UNLINK) un lot d'identifiants int64 de la collection (Cluster-safe).
func (c *Collection) UnlinkMany(ctx context.Context, ids []int64) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return unlinkKeysClusterSafe(ctx, c.client, keys)
}

// DeleteKeys supprime un lot d'identifiants génériques de la collection (Cluster-safe).
func (c *Collection) DeleteKeys(ctx context.Context, ids ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return delKeysClusterSafe(ctx, c.client, keys)
}

// UnlinkKeys supprime via UNLINK un lot d'identifiants génériques de la collection (Cluster-safe).
func (c *Collection) UnlinkKeys(ctx context.Context, ids ...any) error {
	if err := c.checkReady(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	return unlinkKeysClusterSafe(ctx, c.client, keys)
}
