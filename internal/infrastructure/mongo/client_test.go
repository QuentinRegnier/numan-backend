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
