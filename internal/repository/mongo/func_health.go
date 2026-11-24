package mongo

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// FuncPingMongo vérifie la connectivité du cluster MongoDB L2 en mode PrimaryPreferred
// afin d'éviter d'évincer les pods pendant une brève élection Raft (<2s).
func FuncPingMongo(ctx context.Context) error {
	if infraMongo.MongoClient == nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeDatabaseError, "Client MongoDB non initialisé.", nil)
	}
	if err := infraMongo.MongoClient.Ping(ctx, readpref.PrimaryPreferred()); err != nil {
		return numan_error.NewAppError(http.StatusServiceUnavailable, numan_error.CodeDatabaseError, "MongoDB L2 indisponible.", err)
	}
	return nil
}
