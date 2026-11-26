package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

type orphanMetricKey struct {
	tier   string
	entity string
}

type orphanCleanerMetricsState struct {
	mu             sync.RWMutex
	scanned        map[orphanMetricKey]uint64
	skippedGrace   map[orphanMetricKey]uint64
	detected       map[orphanMetricKey]uint64
	unlinked       map[orphanMetricKey]uint64
	errorsByStage  map[string]uint64
	hasDuration    bool
	lastDurationSc float64
}

func newOrphanCleanerMetricsState() *orphanCleanerMetricsState {
	return &orphanCleanerMetricsState{
		scanned:       make(map[orphanMetricKey]uint64),
		skippedGrace:  make(map[orphanMetricKey]uint64),
		detected:      make(map[orphanMetricKey]uint64),
		unlinked:      make(map[orphanMetricKey]uint64),
		errorsByStage: make(map[string]uint64),
	}
}

// RecordRedisOrphanStats incrémente les compteurs Prometheus du Redis Orphan Cleaner pour un couple (tier, entity).
func (r *Registry) RecordRedisOrphanStats(tier, entity string, scanned, skippedGrace, detected, unlinked uint64) {
	if r == nil || r.orphan == nil {
		return
	}
	k := orphanMetricKey{tier: tier, entity: entity}
	r.orphan.mu.Lock()
	r.orphan.scanned[k] += scanned
	r.orphan.skippedGrace[k] += skippedGrace
	r.orphan.detected[k] += detected
	r.orphan.unlinked[k] += unlinked
	r.orphan.mu.Unlock()
}

// RecordRedisOrphanError incrémente le compteur d'erreurs du Redis Orphan Cleaner pour une étape ("scan", "postgres", "unlink").
func (r *Registry) RecordRedisOrphanError(stage string) {
	if r == nil || r.orphan == nil {
		return
	}
	if stage == "" {
		stage = "unknown"
	}
	r.orphan.mu.Lock()
	r.orphan.errorsByStage[stage]++
	r.orphan.mu.Unlock()
}

// RecordRedisOrphanRunDuration enregistre la durée (en secondes) du dernier cycle complet du Redis Orphan Cleaner.
func (r *Registry) RecordRedisOrphanRunDuration(durationSec float64) {
	if r == nil || r.orphan == nil || durationSec < 0 || math.IsNaN(durationSec) || math.IsInf(durationSec, 0) {
		return
	}
	r.orphan.mu.Lock()
	r.orphan.hasDuration = true
	r.orphan.lastDurationSc = durationSec
	r.orphan.mu.Unlock()
}

func (r *Registry) renderOrphanCleanerMetrics(b *strings.Builder) {
	if r == nil || r.orphan == nil {
		return
	}

	r.orphan.mu.RLock()
	keys := make([]orphanMetricKey, 0, len(r.orphan.scanned))
	for k := range r.orphan.scanned {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].tier != keys[j].tier {
			return keys[i].tier < keys[j].tier
		}
		return keys[i].entity < keys[j].entity
	})

	scanned := make([]uint64, len(keys))
	skipped := make([]uint64, len(keys))
	detected := make([]uint64, len(keys))
	unlinked := make([]uint64, len(keys))
	for i, k := range keys {
		scanned[i] = r.orphan.scanned[k]
		skipped[i] = r.orphan.skippedGrace[k]
		detected[i] = r.orphan.detected[k]
		unlinked[i] = r.orphan.unlinked[k]
	}

	stages := make([]string, 0, len(r.orphan.errorsByStage))
	for st := range r.orphan.errorsByStage {
		stages = append(stages, st)
	}
	sort.Strings(stages)
	stageVals := make([]uint64, len(stages))
	for i, st := range stages {
		stageVals[i] = r.orphan.errorsByStage[st]
	}

	hasDur := r.orphan.hasDuration
	lastDur := r.orphan.lastDurationSc
	r.orphan.mu.RUnlock()

	if len(keys) > 0 {
		b.WriteString("# HELP numan_redis_orphan_scanned_total Total Redis keys scanned by the orphan cleaner.\n")
		b.WriteString("# TYPE numan_redis_orphan_scanned_total counter\n")
		for i, k := range keys {
			fmt.Fprintf(b, "numan_redis_orphan_scanned_total{tier=\"%s\",entity=\"%s\"} %d\n",
				escapeLabelValue(k.tier), escapeLabelValue(k.entity), scanned[i])
		}

		b.WriteString("# HELP numan_redis_orphan_skipped_grace_total Total Redis keys skipped due to Snowflake Write-Behind grace window.\n")
		b.WriteString("# TYPE numan_redis_orphan_skipped_grace_total counter\n")
		for i, k := range keys {
			fmt.Fprintf(b, "numan_redis_orphan_skipped_grace_total{tier=\"%s\",entity=\"%s\"} %d\n",
				escapeLabelValue(k.tier), escapeLabelValue(k.entity), skipped[i])
		}

		b.WriteString("# HELP numan_redis_orphan_detected_total Total orphan Redis keys verified absent in PostgreSQL L3.\n")
		b.WriteString("# TYPE numan_redis_orphan_detected_total counter\n")
		for i, k := range keys {
			fmt.Fprintf(b, "numan_redis_orphan_detected_total{tier=\"%s\",entity=\"%s\"} %d\n",
				escapeLabelValue(k.tier), escapeLabelValue(k.entity), detected[i])
		}

		b.WriteString("# HELP numan_redis_orphan_unlinked_total Total orphan Redis keys actually unlinked (0 in dry-run).\n")
		b.WriteString("# TYPE numan_redis_orphan_unlinked_total counter\n")
		for i, k := range keys {
			fmt.Fprintf(b, "numan_redis_orphan_unlinked_total{tier=\"%s\",entity=\"%s\"} %d\n",
				escapeLabelValue(k.tier), escapeLabelValue(k.entity), unlinked[i])
		}
	}

	if len(stages) > 0 {
		b.WriteString("# HELP numan_redis_orphan_errors_total Total Redis orphan cleaner errors by pipeline stage.\n")
		b.WriteString("# TYPE numan_redis_orphan_errors_total counter\n")
		for i, st := range stages {
			fmt.Fprintf(b, "numan_redis_orphan_errors_total{stage=\"%s\"} %d\n",
				escapeLabelValue(st), stageVals[i])
		}
	}

	if hasDur {
		b.WriteString("# HELP numan_redis_orphan_last_run_duration_seconds Duration in seconds of the last Redis orphan cleaner run.\n")
		b.WriteString("# TYPE numan_redis_orphan_last_run_duration_seconds gauge\n")
		fmt.Fprintf(b, "numan_redis_orphan_last_run_duration_seconds %.3f\n", lastDur)
	}
}
