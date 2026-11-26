package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrphanCleanerMetricsRender(t *testing.T) {
	reg := NewRegistry()
	reg.RecordRedisOrphanStats("core", "user", 120, 15, 8, 5)
	reg.RecordRedisOrphanStats("object", "post_physical", 50, 2, 1, 0)
	reg.RecordRedisOrphanError("postgres")
	reg.RecordRedisOrphanError("postgres")
	reg.RecordRedisOrphanError("unlink")
	reg.RecordRedisOrphanRunDuration(1.234)

	out := reg.Render(context.Background())

	assert.Contains(t, out, `numan_redis_orphan_scanned_total{tier="core",entity="user"} 120`)
	assert.Contains(t, out, `numan_redis_orphan_skipped_grace_total{tier="core",entity="user"} 15`)
	assert.Contains(t, out, `numan_redis_orphan_detected_total{tier="core",entity="user"} 8`)
	assert.Contains(t, out, `numan_redis_orphan_unlinked_total{tier="core",entity="user"} 5`)
	assert.Contains(t, out, `numan_redis_orphan_scanned_total{tier="object",entity="post_physical"} 50`)
	assert.Contains(t, out, `numan_redis_orphan_unlinked_total{tier="object",entity="post_physical"} 0`)
	assert.Contains(t, out, `numan_redis_orphan_errors_total{stage="postgres"} 2`)
	assert.Contains(t, out, `numan_redis_orphan_errors_total{stage="unlink"} 1`)
	assert.Contains(t, out, `numan_redis_orphan_last_run_duration_seconds 1.234`)
}
