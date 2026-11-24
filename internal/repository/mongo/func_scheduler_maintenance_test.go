package mongo

import (
	"context"
	"testing"

	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMongoSchedulerMaintenance_NilClientSafe(t *testing.T) {
	orig := infraMongo.MongoClient
	infraMongo.MongoClient = nil
	t.Cleanup(func() { infraMongo.MongoClient = orig })

	deleted, err := FuncPurgeOldSoftDeletes(context.Background(), 1000, []string{"posts", "comments"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)

	stats, err := FuncAuditCollectionStats(context.Background(), []string{"posts", "comments"})
	require.NoError(t, err)
	assert.Empty(t, stats)
}
