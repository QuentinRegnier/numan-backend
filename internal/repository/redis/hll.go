package redis

import (
	"context"
	"errors"
	"time"

	redisgo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	goredis "github.com/redis/go-redis/v9"
)

// PFAdd ajoute un ou plusieurs éléments dans un HyperLogLog Redis.
func (c *Collection) PFAdd(ctx context.Context, id any, els ...any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	if len(els) == 0 {
		return 0, nil
	}
	return c.client.PFAdd(ctx, c.Key(id), els...).Result()
}

// PFCount calcule la cardinalité union d'un ou plusieurs HyperLogLog de la collection (Cluster-safe).
func (c *Collection) PFCount(ctx context.Context, ids ...any) (int64, error) {
	if err := c.checkReady(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}
	if redisgo.IsClusterClient(c.client) && len(keys) > 1 {
		pipe := c.client.Pipeline()
		cmds := make([]*goredis.IntCmd, len(keys))
		for i, k := range keys {
			cmds[i] = pipe.PFCount(ctx, k)
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return 0, err
		}
		var total int64
		for _, cmd := range cmds {
			n, err := cmd.Result()
			if err != nil {
				if errors.Is(err, goredis.Nil) {
					continue
				}
				return 0, err
			}
			total += n
		}
		return total, nil
	}
	return c.client.PFCount(ctx, keys...).Result()
}

// PFCountBatch calcule en pipeline la cardinalité HyperLogLog pour chaque groupe d'identifiants.
// Retourne une tranche alignée positionnellement 1:1 avec `idGroups`.
// En mode Redis Cluster, évite toute erreur CROSSSLOT en exécutant des PFCOUNT unitaires par clé et en agrégeant chaque groupe.
func (c *Collection) PFCountBatch(ctx context.Context, idGroups [][]any) ([]int64, error) {
	if err := c.checkReady(); err != nil {
		return nil, err
	}
	out := make([]int64, len(idGroups))
	if len(idGroups) == 0 {
		return out, nil
	}

	isCluster := redisgo.IsClusterClient(c.client)
	const groupChunkSize = 250

	for start := 0; start < len(idGroups); start += groupChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + groupChunkSize
		if end > len(idGroups) {
			end = len(idGroups)
		}
		chunk := idGroups[start:end]

		if !isCluster {
			pipe := c.client.Pipeline()
			cmds := make([]*goredis.IntCmd, len(chunk))
			for i, group := range chunk {
				if len(group) == 0 {
					continue
				}
				keys := make([]string, len(group))
				for j, id := range group {
					keys[j] = c.Key(id)
				}
				cmds[i] = pipe.PFCount(ctx, keys...)
			}
			if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
				return nil, err
			}
			for i, cmd := range cmds {
				if cmd == nil {
					continue
				}
				n, err := cmd.Result()
				if err != nil {
					if errors.Is(err, goredis.Nil) {
						continue
					}
					return nil, err
				}
				out[start+i] = n
			}
			continue
		}

		// Mode Cluster : 1 PFCOUNT par clé unitaire (zéro CROSSSLOT) puis agrégation par groupe.
		pipe := c.client.Pipeline()
		groupCmds := make([][]*goredis.IntCmd, len(chunk))
		for i, group := range chunk {
			if len(group) == 0 {
				continue
			}
			cmds := make([]*goredis.IntCmd, len(group))
			for j, id := range group {
				cmds[j] = pipe.PFCount(ctx, c.Key(id))
			}
			groupCmds[i] = cmds
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}
		for i, cmds := range groupCmds {
			var sum int64
			for _, cmd := range cmds {
				n, err := cmd.Result()
				if err != nil {
					if errors.Is(err, goredis.Nil) {
						continue
					}
					return nil, err
				}
				sum += n
			}
			out[start+i] = sum
		}
	}

	return out, nil
}

// RecordCommunityMessageActivityPipeline incrémente en un pipeline le compteur journalier (HASH)
// et ajoute l'auteur dans le HyperLogLog journalier avec EXPIRE ... NX.
// En cas d'échec de EXPIRE ... NX (ex: Redis < 7), seuls les EXPIRE en erreur sont rejoués sans jamais
// relancer HINCRBY ni PFADD (zéro double-comptage). Si les deux collections pointent sur des tiers distincts,
// chaque client exécute son propre pipeline.
func RecordCommunityMessageActivityPipeline(
	ctx context.Context,
	msgCountCol *Collection,
	authorsCol *Collection,
	dateBucket string,
	authorsSubKey string,
	convField string,
	senderVal string,
	ttl time.Duration,
) error {
	if err := msgCountCol.checkReady(); err != nil {
		return err
	}
	if err := authorsCol.checkReady(); err != nil {
		return err
	}

	ttlSec := int64(ttl / time.Second)
	if ttlSec <= 0 {
		ttl = 8 * 24 * time.Hour
		ttlSec = int64(ttl / time.Second)
	}

	msgCountKey := msgCountCol.Key(dateBucket)
	authorsKey := authorsCol.Key(authorsSubKey)

	if msgCountCol.client == authorsCol.client {
		pipe := msgCountCol.client.Pipeline()
		hincrCmd := pipe.HIncrBy(ctx, msgCountKey, convField, 1)
		msgExpireCmd := pipe.Do(ctx, "EXPIRE", msgCountKey, ttlSec, "NX")
		pfaddCmd := pipe.PFAdd(ctx, authorsKey, senderVal)
		authorsExpireCmd := pipe.Do(ctx, "EXPIRE", authorsKey, ttlSec, "NX")

		if _, err := pipe.Exec(ctx); err != nil {
			if errH := hincrCmd.Err(); errH != nil {
				return errH
			}
			if errP := pfaddCmd.Err(); errP != nil {
				return errP
			}

			var fallbackKeys []string
			if msgExpireCmd.Err() != nil {
				fallbackKeys = append(fallbackKeys, msgCountKey)
			}
			if authorsExpireCmd.Err() != nil {
				fallbackKeys = append(fallbackKeys, authorsKey)
			}
			if len(fallbackKeys) == 0 {
				return err
			}

			fallbackPipe := msgCountCol.client.Pipeline()
			for _, k := range fallbackKeys {
				fallbackPipe.Expire(ctx, k, ttl)
			}
			if _, fbErr := fallbackPipe.Exec(ctx); fbErr != nil {
				return fbErr
			}
		}
		return nil
	}

	// Collections sur des clients Redis distincts : un pipeline par client, même garantie anti double-comptage.
	msgPipe := msgCountCol.client.Pipeline()
	hincrCmd := msgPipe.HIncrBy(ctx, msgCountKey, convField, 1)
	msgExpireCmd := msgPipe.Do(ctx, "EXPIRE", msgCountKey, ttlSec, "NX")
	if _, err := msgPipe.Exec(ctx); err != nil {
		if errH := hincrCmd.Err(); errH != nil {
			return errH
		}
		if msgExpireCmd.Err() == nil {
			return err
		}
		if fbErr := msgCountCol.client.Expire(ctx, msgCountKey, ttl).Err(); fbErr != nil {
			return fbErr
		}
	}

	authorsPipe := authorsCol.client.Pipeline()
	pfaddCmd := authorsPipe.PFAdd(ctx, authorsKey, senderVal)
	authorsExpireCmd := authorsPipe.Do(ctx, "EXPIRE", authorsKey, ttlSec, "NX")
	if _, err := authorsPipe.Exec(ctx); err != nil {
		if errP := pfaddCmd.Err(); errP != nil {
			return errP
		}
		if authorsExpireCmd.Err() == nil {
			return err
		}
		if fbErr := authorsCol.client.Expire(ctx, authorsKey, ttl).Err(); fbErr != nil {
			return fbErr
		}
	}
	return nil
}
