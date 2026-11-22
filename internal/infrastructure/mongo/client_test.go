package mongo_test

import (
	"context"
	"testing"

	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestEnsureIndexes(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success_all_collections_including_user_reco_skips", func(mt *mtest.T) {
		// EnsureIndexes calls CreateMany / CreateOne 28 times across all collections
		for i := 0; i < 30; i++ {
			mt.AddMockResponses(mtest.CreateSuccessResponse())
		}

		err := infraMongo.EnsureIndexes(context.Background(), mt.DB)
		require.NoError(mt, err)
	})

	mt.Run("propagates_index_creation_errors", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{
			Code:    85,
			Message: "IndexOptionsConflict",
		}))
		for i := 0; i < 30; i++ {
			mt.AddMockResponses(mtest.CreateSuccessResponse())
		}

		err := infraMongo.EnsureIndexes(context.Background(), mt.DB)
		assert.Error(mt, err)
	})
}

func clearMongoEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MONGO_URI", "MONGO_HOST", "MONGO_HOSTS", "MONGO_PORT", "MONGO_DB",
		"MONGO_APP_USER", "MONGO_APP_PASSWORD",
		"MONGO_ROOT_USER", "MONGO_ROOT_PASSWORD",
		"MONGO_AUTH_SOURCE", "MONGO_TLS",
		"MONGO_REPLICA_SET", "MONGO_READ_PREFERENCE",
		"MONGO_LOCAL_THRESHOLD_MS", "MONGO_MAX_STALENESS_SEC",
		"MONGO_WRITE_CONCERN", "MONGO_MAX_POOL_SIZE", "MONGO_MIN_POOL_SIZE",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveMongoURI_AppUserHostPortTLSAndReadPreference(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_HOST", "mongo-headless.numan.svc.cluster.local")
	t.Setenv("MONGO_PORT", "27018")
	t.Setenv("MONGO_DB", "numan")
	t.Setenv("MONGO_APP_USER", "numan_app")
	t.Setenv("MONGO_APP_PASSWORD", "super@secret#pass")
	t.Setenv("MONGO_ROOT_USER", "root")
	t.Setenv("MONGO_ROOT_PASSWORD", "rootpass")
	t.Setenv("MONGO_TLS", "true")
	t.Setenv("MONGO_READ_PREFERENCE", "nearest")

	uri := infraMongo.ResolveMongoURI()
	assert.Contains(t, uri, "mongodb://numan_app:super%40secret%23pass@mongo-headless.numan.svc.cluster.local:27018/numan?")
	assert.Contains(t, uri, "authSource=numan")
	assert.Contains(t, uri, "tls=true")
	assert.Contains(t, uri, "readPreference=nearest")
	assert.NotContains(t, uri, "rootpass")
}

func TestResolveMongoURI_FallbackRootAndExplicitURI(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_ROOT_USER", "adminuser")
	t.Setenv("MONGO_ROOT_PASSWORD", "adminpass")

	uri := infraMongo.ResolveMongoURI()
	assert.Equal(t, "mongodb://adminuser:adminpass@mongo:27017/numan_mongo?authSource=admin", uri)

	t.Setenv("MONGO_URI", "mongodb://u:p@m0:27017,m1:27017/numan?replicaSet=rs0&readPreference=nearest")
	assert.Equal(t, "mongodb://u:p@m0:27017,m1:27017/numan?replicaSet=rs0&readPreference=nearest", infraMongo.ResolveMongoURI())
}

func TestResolveMongoConfig_ReplicaSetMultiHostRTTRoutingAndOptions(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_HOSTS", "mongo-0.mongo-headless:27017, mongo-1.mongo-headless, mongo-2.mongo-headless:27017")
	t.Setenv("MONGO_PORT", "27017")
	t.Setenv("MONGO_DB", "numan_mongo")
	t.Setenv("MONGO_APP_USER", "numan_app")
	t.Setenv("MONGO_APP_PASSWORD", "s3cr3t!")
	t.Setenv("MONGO_REPLICA_SET", "rs0")
	t.Setenv("MONGO_READ_PREFERENCE", "nearest")
	t.Setenv("MONGO_LOCAL_THRESHOLD_MS", "20")
	t.Setenv("MONGO_MAX_STALENESS_SEC", "120")
	t.Setenv("MONGO_MAX_POOL_SIZE", "80")
	t.Setenv("MONGO_MIN_POOL_SIZE", "10")

	cfg := infraMongo.ResolveMongoConfig()
	assert.Contains(t, cfg.URI, "mongodb://numan_app:s3cr3t%21@mongo-0.mongo-headless:27017,mongo-1.mongo-headless:27017,mongo-2.mongo-headless:27017/numan_mongo?")
	assert.Contains(t, cfg.URI, "replicaSet=rs0")
	assert.NotContains(t, cfg.URI, "%2C")
	assert.Equal(t, "rs0", cfg.ReplicaSet)
	assert.Equal(t, "nearest", cfg.ReadPreference)
	assert.Equal(t, "majority", cfg.WriteConcern)

	opts, err := infraMongo.BuildMongoClientOptions(cfg)
	require.NoError(t, err)
	require.NotNil(t, opts.LocalThreshold)
	assert.Equal(t, int64(20), opts.LocalThreshold.Milliseconds())
	require.NotNil(t, opts.RetryWrites)
	assert.True(t, *opts.RetryWrites)
	require.NotNil(t, opts.RetryReads)
	assert.True(t, *opts.RetryReads)
	require.NotNil(t, opts.MaxPoolSize)
	assert.Equal(t, uint64(80), *opts.MaxPoolSize)
	require.NotNil(t, opts.MinPoolSize)
	assert.Equal(t, uint64(10), *opts.MinPoolSize)
	require.NotNil(t, opts.ReadPreference)
	assert.Equal(t, "nearest", opts.ReadPreference.Mode().String())
	maxStale, ok := opts.ReadPreference.MaxStaleness()
	require.True(t, ok)
	assert.Equal(t, int64(120), int64(maxStale.Seconds()))
	require.NotNil(t, opts.WriteConcern)
	assert.Equal(t, "majority", opts.WriteConcern.W)
}

func TestResolveMongoConfig_ReplicaSetDefaultSecondaryPreferredAndMinStalenessClamp(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_HOSTS", "m0:27017,m1:27017,m2:27017")
	t.Setenv("MONGO_REPLICA_SET", "rs0")
	t.Setenv("MONGO_MAX_STALENESS_SEC", "30") // inférieur au minimum MongoDB 90s -> clampé à 90s

	cfg := infraMongo.ResolveMongoConfig()
	assert.Equal(t, "secondaryPreferred", cfg.ReadPreference)
	assert.Equal(t, int64(15), cfg.LocalThreshold.Milliseconds())
	assert.Equal(t, int64(90), int64(cfg.MaxStaleness.Seconds()))

	opts, err := infraMongo.BuildMongoClientOptions(cfg)
	require.NoError(t, err)
	require.NotNil(t, opts.ReadPreference)
	assert.Equal(t, "secondaryPreferred", opts.ReadPreference.Mode().String())
}

func TestRedactMongoURI_NeverLeaksPassword(t *testing.T) {
	redacted := infraMongo.RedactMongoURI("mongodb://numan_app:TopSecretPassword123!@mongo:27017/numan?authSource=numan")
	assert.NotContains(t, redacted, "TopSecretPassword123!")
	assert.NotContains(t, redacted, "numan_app")
	assert.Equal(t, "mongodb://mongo:27017/numan", redacted)

	multiRedacted := infraMongo.RedactMongoURI("mongodb://numan_app:TopSecret!@mongo-0.mongo-headless:27017,mongo-1.mongo-headless:27017,mongo-2.mongo-headless:27017/numan_mongo?replicaSet=rs0")
	assert.NotContains(t, multiRedacted, "TopSecret!")
	assert.NotContains(t, multiRedacted, "numan_app")
	assert.Equal(t, "mongodb://mongo-0.mongo-headless:27017,mongo-1.mongo-headless:27017,mongo-2.mongo-headless:27017/numan_mongo", multiRedacted)

	assert.Equal(t, "<redacted-mongo-uri>", infraMongo.RedactMongoURI("://broken\x7furi"))
}
