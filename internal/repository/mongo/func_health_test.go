package mongo

import (
	"context"
	"testing"

	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/stretchr/testify/require"
)

func TestFuncPingMongo_NilClientReturnsError(t *testing.T) {
	orig := infraMongo.MongoClient
	infraMongo.MongoClient = nil
	t.Cleanup(func() { infraMongo.MongoClient = orig })

	err := FuncPingMongo(context.Background())
	require.Error(t, err)
}
