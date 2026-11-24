package cache_service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/redis/go-redis/v9"
)

const (
	schedulerKeyGlobalPaused   = "global:paused"
	schedulerKeyGlobalOverload = "global:overload"
	schedulerPrefixRemoteTrig  = "trigger:"
	schedulerPrefixJobStatus   = "job:"
)

// AcquireSchedulerLocalLock acquiert le verrou distribué Redis Core local au datacenter pour un job.
func AcquireSchedulerLocalLock(ctx context.Context, jobName string, ttl time.Duration) (string, bool, error) {
	return repoRedis.AcquireCronLock(ctx, "scheduler:"+jobName, ttl)
}

// ReleaseSchedulerLocalLock libère le verrou distribué Redis Core local au datacenter pour un job.
func ReleaseSchedulerLocalLock(ctx context.Context, jobName, token string) error {
	return repoRedis.ReleaseCronLock(ctx, "scheduler:"+jobName, token)
}

// SetSchedulerOverloadFlag publie ou efface le drapeau global `scheduler:global:overload` dans Redis Core.
func SetSchedulerOverloadFlag(ctx context.Context, active bool, ttl time.Duration) error {
	if repoRedis.SchedulerState == nil {
		return nil
	}
	if active {
		return repoRedis.SchedulerState.SetRawWithTTL(ctx, schedulerKeyGlobalOverload, "1", ttl)
	}
	return repoRedis.SchedulerState.DeletePrimitive(ctx, schedulerKeyGlobalOverload)
}

// IsSchedulerOverloadFlagSet indique si le drapeau global `scheduler:global:overload` est actif dans Redis Core.
func IsSchedulerOverloadFlagSet(ctx context.Context) bool {
	if repoRedis.SchedulerState == nil {
		return false
	}
	raw, err := repoRedis.SchedulerState.GetBytes(ctx, schedulerKeyGlobalOverload)
	return err == nil && len(raw) > 0
}

// SetSchedulerGlobalPause active la pause globale du Scheduler dans Redis Core avec un TTL auto-expirant.
func SetSchedulerGlobalPause(ctx context.Context, payload scheduler_models.PausePayload, ttl time.Duration) error {
	if repoRedis.SchedulerState == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeInternalError, "Échec de sérialisation de la pause scheduler.", err)
	}
	if err := repoRedis.SchedulerState.SetRawWithTTL(ctx, schedulerKeyGlobalPaused, raw, ttl); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible d'activer la pause globale du scheduler.", err)
	}
	return nil
}

// ClearSchedulerGlobalPause supprime la pause globale du Scheduler dans Redis Core.
func ClearSchedulerGlobalPause(ctx context.Context) error {
	if repoRedis.SchedulerState == nil {
		return nil
	}
	if err := repoRedis.SchedulerState.DeletePrimitive(ctx, schedulerKeyGlobalPaused); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de lever la pause globale du scheduler.", err)
	}
	return nil
}

// GetSchedulerGlobalPause lit l'état de pause globale depuis Redis Core.
// Retourne (paused, reason, redisAuthoritative).
func GetSchedulerGlobalPause(ctx context.Context) (bool, string, bool) {
	if repoRedis.SchedulerState == nil {
		return false, "", false
	}
	raw, err := repoRedis.SchedulerState.GetBytes(ctx, schedulerKeyGlobalPaused)
	if err == nil && len(raw) > 0 {
		var p scheduler_models.PausePayload
		if json.Unmarshal(raw, &p) == nil && p.Reason != "" {
			return true, p.Reason, true
		}
		return true, "paused", true
	}
	if errors.Is(err, goredis.Nil) {
		return false, "", true
	}
	return false, "", false
}

// SaveSchedulerJobStatus persiste le statut et le checkpoint d'un job dans Redis Core (`scheduler:job:<name>`).
func SaveSchedulerJobStatus(ctx context.Context, st scheduler_models.JobStatus, ttl time.Duration) error {
	if repoRedis.SchedulerState == nil {
		return nil
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return repoRedis.SchedulerState.SetRawWithTTL(ctx, schedulerPrefixJobStatus+st.Name, raw, ttl)
}

// GetSchedulerJobStatus lit le dernier statut persisté d'un job dans Redis Core.
func GetSchedulerJobStatus(ctx context.Context, name string) (*scheduler_models.JobStatus, bool) {
	if repoRedis.SchedulerState == nil {
		return nil, false
	}
	raw, err := repoRedis.SchedulerState.GetBytes(ctx, schedulerPrefixJobStatus+name)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var st scheduler_models.JobStatus
	if json.Unmarshal(raw, &st) != nil {
		return nil, false
	}
	return &st, true
}

// ListAllSchedulerJobStatuses scanne et charge tous les statuts de jobs persistés dans Redis Core.
func ListAllSchedulerJobStatuses(ctx context.Context) ([]scheduler_models.JobStatus, error) {
	if repoRedis.SchedulerState == nil {
		return []scheduler_models.JobStatus{}, nil
	}
	prefix := repoRedis.SchedulerState.Prefix + ":" + schedulerPrefixJobStatus
	keys, err := repoRedis.ScanKeysOnCore(ctx, prefix+"*", 200)
	if err != nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Échec du scan des statuts scheduler.", err)
	}
	if len(keys) == 0 {
		return []scheduler_models.JobStatus{}, nil
	}
	rawList, err := repoRedis.GetBytesBatchOnCore(ctx, keys)
	if err != nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Échec de lecture des statuts scheduler.", err)
	}
	out := make([]scheduler_models.JobStatus, 0, len(rawList))
	for _, raw := range rawList {
		var st scheduler_models.JobStatus
		if json.Unmarshal(raw, &st) == nil {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// EnqueueSchedulerRemoteTrigger dépose une commande de déclenchement manuel dans Redis Core pour le pod scheduler_node.
func EnqueueSchedulerRemoteTrigger(ctx context.Context, req scheduler_models.RemoteTriggerPayload, ttl time.Duration) error {
	if repoRedis.SchedulerState == nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeServiceUnavailable, "Scheduler indisponible.", nil)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeInternalError, "Échec de sérialisation du déclenchement distant.", err)
	}
	if err := repoRedis.SchedulerState.SetRawWithTTL(ctx, schedulerPrefixRemoteTrig+req.JobName, raw, ttl); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible de planifier le déclenchement distant du job.", err)
	}
	return nil
}

// PopSchedulerRemoteTrigger consomme une éventuelle commande de déclenchement distant pour le job donné.
func PopSchedulerRemoteTrigger(ctx context.Context, jobName string) (*scheduler_models.RemoteTriggerPayload, bool) {
	if repoRedis.SchedulerState == nil {
		return nil, false
	}
	key := schedulerPrefixRemoteTrig + jobName
	raw, err := repoRedis.SchedulerState.GetBytes(ctx, key)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	_ = repoRedis.SchedulerState.DeletePrimitive(ctx, key)
	var req scheduler_models.RemoteTriggerPayload
	if json.Unmarshal(raw, &req) != nil {
		return nil, false
	}
	return &req, true
}

// PruneSyncLedgersBefore purge les entrées ZSET antérieures à cutoffMs dans UserSyncLedger et ConvMessageLedger.
func PruneSyncLedgersBefore(ctx context.Context, cutoffMs int64, onStep func(done, total int64) error) (int64, int64, error) {
	maxScore := fmt.Sprintf("%d", cutoffMs)
	var prefixes []string
	if repoRedis.UserSyncLedger != nil {
		prefixes = append(prefixes, repoRedis.UserSyncLedger.Prefix+"*")
	}
	if repoRedis.ConvMessageLedger != nil {
		prefixes = append(prefixes, repoRedis.ConvMessageLedger.Prefix+"*")
	}
	var done, total int64
	for _, pattern := range prefixes {
		keys, err := repoRedis.ScanKeysOnCore(ctx, pattern, 200)
		if err != nil {
			return done, total, err
		}
		total += int64(len(keys))
		for _, k := range keys {
			if ctx.Err() != nil {
				return done, total, ctx.Err()
			}
			_, _ = repoRedis.ZRemRangeByScoreOnCore(ctx, k, "-inf", maxScore)
			done++
			if onStep != nil {
				if err := onStep(done, total); err != nil {
					return done, total, err
				}
			}
		}
	}
	return done, total, nil
}

// PruneDeadGraphEdges nettoie les arêtes Markov mortes (< minWeight) dans `graph_cache:tag_edges:*` et supprime les clés vides.
func PruneDeadGraphEdges(ctx context.Context, minWeight float64, onStep func(done, total int64) error) (int64, int64, error) {
	if repoRedis.GraphEdges == nil {
		return 0, 0, nil
	}
	keys, err := repoRedis.ScanKeysOnCore(ctx, repoRedis.GraphEdges.Prefix+"*", 200)
	if err != nil {
		return 0, 0, err
	}
	total := int64(len(keys))
	var done int64
	maxScore := fmt.Sprintf("%f", minWeight)
	for _, k := range keys {
		if ctx.Err() != nil {
			return done, total, ctx.Err()
		}
		_, _ = repoRedis.ZRemRangeByScoreOnCore(ctx, k, "-inf", maxScore)
		if card, errC := repoRedis.ZCardOnCore(ctx, k); errC == nil && card == 0 {
			_ = repoRedis.DeleteKeyOnCore(ctx, k)
		}
		done++
		if onStep != nil {
			if err := onStep(done, total); err != nil {
				return done, total, err
			}
		}
	}
	return done, total, nil
}

// ScanArticlesIndexRaw lit l'intégralité du HASH `articles:index:data` sur Tier 2 Redis Object Pool.
func ScanArticlesIndexRaw(ctx context.Context) (map[string]string, error) {
	return repoRedis.HGetAllOnObjects(ctx, variables.RedisKeyArticlesIndexData)
}

// AuditLexAutocompleteCaches audite les ZSET lexicographiques TTL-0 (UsersLex, CommunitiesLex, TagsLex) et supprime les entrées corrompues.
func AuditLexAutocompleteCaches(ctx context.Context) (int64, int64, error) {
	if repoRedis.UsersLex == nil || repoRedis.CommunitiesLex == nil || repoRedis.TagsLex == nil {
		return 0, 0, nil
	}
	colls := []*repoRedis.Collection{repoRedis.UsersLex, repoRedis.CommunitiesLex, repoRedis.TagsLex}
	var done, total int64
	for _, coll := range colls {
		if ctx.Err() != nil {
			return done, total, ctx.Err()
		}
		members, err := coll.ZRange(ctx, variables.LexicographicGlobalKey, 0, 999)
		if err != nil {
			continue
		}
		total += int64(len(members))
		for _, m := range members {
			if ctx.Err() != nil {
				return done, total, ctx.Err()
			}
			trimmed := strings.TrimSpace(m)
			if trimmed == "" || (coll != repoRedis.TagsLex && !strings.Contains(trimmed, ":")) {
				_ = coll.ZRem(ctx, variables.LexicographicGlobalKey, m)
			}
			done++
		}
	}
	return done, total, nil
}

// InvalidateUserProfilesFromCache invalide les entrées L1 Users et UsersLite après réconciliation de ban temporaire.
func InvalidateUserProfilesFromCache(ctx context.Context, userID int64) {
	if repoRedis.Users != nil {
		_ = repoRedis.Users.DeleteObject(ctx, userID)
	}
	if repoRedis.UsersLite != nil {
		_ = repoRedis.UsersLite.DeleteObject(ctx, userID)
	}
}

type dlqEntryEnvelope struct {
	Event     *repoRedis.AsyncEvent `json:"event,omitempty"`
	Retry     int                   `json:"retry,omitempty"`
	Timestamp int64                 `json:"ts,omitempty"`
}

// AuditAndReplayDLQ dépile jusqu'à maxBatch éléments de `redis.DLQ`, rejoue les événements transitoires récupérables et purge les entrées expirées.
func AuditAndReplayDLQ(ctx context.Context, maxBatch int64, cutoffMs int64, maxRetries int) (int64, int64, error) {
	if repoRedis.DLQ == nil {
		return 0, 0, nil
	}
	dlqKey := repoRedis.DLQ.Key("global")
	length, err := repoRedis.LLenOnCore(ctx, dlqKey)
	if err != nil || length == 0 {
		return 0, 0, nil
	}
	limit := maxBatch
	if length < limit {
		limit = length
	}

	var done int64
	var keep []any
	for i := int64(0); i < limit; i++ {
		if ctx.Err() != nil {
			if len(keep) > 0 {
				_ = repoRedis.RPushOnCore(context.Background(), dlqKey, keep...)
			}
			return done, limit, ctx.Err()
		}
		raw, errPop := repoRedis.LPopOnCore(ctx, dlqKey)
		if errPop != nil || raw == "" {
			break
		}
		done++

		var env dlqEntryEnvelope
		if json.Unmarshal([]byte(raw), &env) == nil && env.Event != nil {
			if env.Event.Timestamp > 0 && env.Event.Timestamp < cutoffMs {
				continue
			}
			if env.Retry < maxRetries {
				_ = repoRedis.EnqueueDB(ctx, env.Event.ID, env.Event.ID, env.Event.Type, env.Event.Action, env.Event.Payload, env.Event.Targets)
				continue
			}
			keep = append(keep, raw)
			continue
		}

		var direct repoRedis.AsyncEvent
		if json.Unmarshal([]byte(raw), &direct) == nil && direct.ID != 0 {
			if direct.Timestamp > 0 && direct.Timestamp < cutoffMs {
				continue
			}
			_ = repoRedis.EnqueueDB(ctx, direct.ID, direct.ID, direct.Type, direct.Action, direct.Payload, direct.Targets)
			continue
		}
		keep = append(keep, raw)
	}
	if len(keep) > 0 {
		_ = repoRedis.RPushOnCore(ctx, dlqKey, keep...)
	}
	return done, limit, nil
}

// PruneLSHAffinityBuckets purge les IDs de posts supprimés/masqués (`Visibility < 0`) des buckets LSH.
func PruneLSHAffinityBuckets(ctx context.Context, onStep func(done, total int64) error) (int64, int64, error) {
	if repoRedis.LSHBuckets == nil {
		return 0, 0, nil
	}
	keys, err := repoRedis.ScanKeysOnCore(ctx, repoRedis.LSHBuckets.Prefix+"*", 200)
	if err != nil {
		return 0, 0, err
	}
	total := int64(len(keys))
	var done int64
	for _, key := range keys {
		if ctx.Err() != nil {
			return done, total, ctx.Err()
		}
		members, errM := repoRedis.SMembersOnCore(ctx, key)
		if errM != nil {
			continue
		}
		for _, m := range members {
			if ctx.Err() != nil {
				return done, total, ctx.Err()
			}
			pid, errP := strconv.ParseInt(m, 10, 64)
			if errP != nil || pid <= 0 {
				_, _ = repoRedis.SRemOnCore(ctx, key, m)
				continue
			}
			if repoRedis.Posts != nil {
				var p post_models.PostPayload
				if errGet := repoRedis.Posts.GetObject(ctx, pid, &p); errGet == nil && p.Visibility < 0 {
					_, _ = repoRedis.SRemOnCore(ctx, key, m)
				}
			}
		}
		done++
		if onStep != nil {
			if err := onStep(done, total); err != nil {
				return done, total, err
			}
		}
	}
	return done, total, nil
}

// EnrichSocialGraph2HopSuggestions parcourt les relations 1-hop/2-hop hors-ligne et enrichit `SpeedAddable` pour les affinités >= 2 amis communs.
func EnrichSocialGraph2HopSuggestions(ctx context.Context, onStep func(done, total int64) error) (int64, int64, error) {
	if repoRedis.SpeedRelations == nil || repoRedis.SpeedAddable == nil {
		return 0, 0, nil
	}
	keys, err := repoRedis.ScanKeysOnCore(ctx, repoRedis.SpeedRelations.Prefix+"*", 200)
	if err != nil {
		return 0, 0, err
	}
	total := int64(len(keys))
	var done int64
	for _, k := range keys {
		if ctx.Err() != nil {
			return done, total, ctx.Err()
		}
		uidStr := strings.TrimPrefix(k, repoRedis.SpeedRelations.Prefix+":")
		uid, errU := strconv.ParseInt(uidStr, 10, 64)
		if errU != nil || uid <= 0 {
			done++
			continue
		}
		hop1, errH1 := repoRedis.SpeedRelations.SMembers(ctx, uid)
		if errH1 != nil || len(hop1) == 0 {
			done++
			continue
		}
		direct := make(map[string]struct{}, len(hop1)+1)
		direct[uidStr] = struct{}{}
		for _, f := range hop1 {
			direct[f] = struct{}{}
		}

		mutualCounts := make(map[string]int)
		limitHop1 := len(hop1)
		if limitHop1 > 50 {
			limitHop1 = 50
		}
		for _, f := range hop1[:limitHop1] {
			if ctx.Err() != nil {
				return done, total, ctx.Err()
			}
			fID, errF := strconv.ParseInt(f, 10, 64)
			if errF != nil {
				continue
			}
			hop2, errH2 := repoRedis.SpeedRelations.SMembers(ctx, fID)
			if errH2 != nil {
				continue
			}
			for _, candidate := range hop2 {
				if _, already := direct[candidate]; already {
					continue
				}
				mutualCounts[candidate]++
			}
		}
		for cand, cnt := range mutualCounts {
			if cnt >= 2 {
				_ = repoRedis.SpeedAddable.SAdd(ctx, uid, cand)
			}
		}
		done++
		if onStep != nil {
			if err := onStep(done, total); err != nil {
				return done, total, err
			}
		}
	}
	return done, total, nil
}

// SurgicallyPurifyMostCache retire UNIQUEMENT les posts supprimés/masqués (`Visibility < 0`) de RankedPosts et TagPosts
// sans jamais altérer ni réinitialiser les scores viraux convergés des posts valides.
func SurgicallyPurifyMostCache(ctx context.Context, onStep func(done, total int64) error) (int64, int64, error) {
	if repoRedis.RankedPosts == nil || repoRedis.Posts == nil {
		return 0, 0, nil
	}
	zsetPrefixes := []string{repoRedis.RankedPosts.Prefix + "*"}
	if repoRedis.TagPosts != nil {
		zsetPrefixes = append(zsetPrefixes, repoRedis.TagPosts.Prefix+"*")
	}

	var done, total int64
	for _, pattern := range zsetPrefixes {
		keys, err := repoRedis.ScanKeysOnCore(ctx, pattern, 200)
		if err != nil {
			return done, total, err
		}
		total += int64(len(keys))
		for _, key := range keys {
			if ctx.Err() != nil {
				return done, total, ctx.Err()
			}
			members, errM := repoRedis.ZRangeOnCore(ctx, key, 0, 499)
			if errM != nil {
				continue
			}
			for _, m := range members {
				if ctx.Err() != nil {
					return done, total, ctx.Err()
				}
				pid, errP := strconv.ParseInt(m, 10, 64)
				if errP != nil || pid <= 0 {
					continue
				}
				var post post_models.PostPayload
				if errGet := repoRedis.Posts.GetObject(ctx, pid, &post); errGet == nil && post.Visibility < 0 {
					_, _ = repoRedis.ZRemOnCore(ctx, key, m)
				}
			}
			done++
			if onStep != nil {
				if err := onStep(done, total); err != nil {
					return done, total, err
				}
			}
		}
	}
	return done, total, nil
}

// ObjectPoolMaxFillRatio retourne le taux de remplissage maximal observé sur les shards du Tier 2 Redis Object Pool.
func ObjectPoolMaxFillRatio(ctx context.Context) float64 {
	_, objTel, _ := repoRedis.CollectAllTiersTelemetry(ctx)
	maxFill := 0.0
	for _, shard := range objTel.Shards {
		if shard.MaxBytes > 0 && shard.FillRatio > maxFill {
			maxFill = shard.FillRatio
		}
	}
	return maxFill
}

// GetTopRecentRankedPostIDs retourne les top IDs de posts récents du MOST Cache (langue par défaut) pour pré-chauffage matinal.
func GetTopRecentRankedPostIDs(ctx context.Context, limit int64) ([]int64, error) {
	if repoRedis.RankedPosts == nil || limit <= 0 {
		return nil, nil
	}
	rawIDs, err := repoRedis.RankedPosts.ZRevRange(ctx, variables.StrictRankSubKey(variables.StrictTypeRecent, variables.DefaultContentLanguage), 0, limit-1)
	if err != nil || len(rawIDs) == 0 {
		return nil, err
	}
	ids := make([]int64, 0, len(rawIDs))
	for _, s := range rawIDs {
		if id, errP := strconv.ParseInt(s, 10, 64); errP == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
