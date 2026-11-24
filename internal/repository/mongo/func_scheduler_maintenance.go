package mongo

import (
	"context"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoCollectionAuditStat résume la taille logique/stockage d'une collection MongoDB L2.
type MongoCollectionAuditStat struct {
	Collection   string
	Count        int64
	SizeBytes    int64
	StorageBytes int64
}

// FuncPurgeOldSoftDeletes supprime dans MongoDB L2 les documents soft-deleted plus anciens que cutoffMs.
func FuncPurgeOldSoftDeletes(ctx context.Context, cutoffMs int64, collections []string) (int64, error) {
	if infraMongo.MongoClient == nil {
		return 0, nil
	}
	db := infraMongo.MongoClient.Database(variables.MongoDatabaseName)
	var totalDeleted int64
	filter := bson.M{
		"visibility": -1,
		"updated_at": bson.M{"$lt": cutoffMs},
	}
	for _, collName := range collections {
		if ctx.Err() != nil {
			return totalDeleted, ctx.Err()
		}
		res, err := db.Collection(collName).DeleteMany(ctx, filter)
		if err != nil {
			return totalDeleted, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeDatabaseError, "Erreur purge soft-deletes MongoDB.", err)
		}
		if res != nil {
			totalDeleted += res.DeletedCount
		}
	}
	return totalDeleted, nil
}

// FuncAuditCollectionStats exécute la commande collStats sur chaque collection demandée pour l'audit d'hygiène L2.
func FuncAuditCollectionStats(ctx context.Context, collections []string) ([]MongoCollectionAuditStat, error) {
	if infraMongo.MongoClient == nil {
		return nil, nil
	}
	db := infraMongo.MongoClient.Database(variables.MongoDatabaseName)
	out := make([]MongoCollectionAuditStat, 0, len(collections))
	for _, collName := range collections {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		var stats struct {
			Size        int64 `bson:"size"`
			StorageSize int64 `bson:"storageSize"`
			Count       int64 `bson:"count"`
		}
		if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: collName}}).Decode(&stats); err != nil {
			continue
		}
		out = append(out, MongoCollectionAuditStat{
			Collection:   collName,
			Count:        stats.Count,
			SizeBytes:    stats.Size,
			StorageBytes: stats.StorageSize,
		})
	}
	return out, nil
}

// FuncIsSecondaryNode vérifie si le nœud MongoDB courant est un secondaire du ReplicaSet (seul cas autorisant `compact`).
func FuncIsSecondaryNode(ctx context.Context) bool {
	if infraMongo.MongoClient == nil {
		return false
	}
	db := infraMongo.MongoClient.Database(variables.MongoDatabaseName)
	var helloRes bson.M
	if err := db.RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&helloRes); err != nil {
		return false
	}
	sec, ok := helloRes["secondary"].(bool)
	return ok && sec
}

// FuncCompactCollectionIfSecondary exécute `compact` sur une collection uniquement si le nœud courant est un secondaire.
func FuncCompactCollectionIfSecondary(ctx context.Context, collectionName string) error {
	if infraMongo.MongoClient == nil || !FuncIsSecondaryNode(ctx) {
		return nil
	}
	db := infraMongo.MongoClient.Database(variables.MongoDatabaseName)
	return db.RunCommand(ctx, bson.D{{Key: "compact", Value: collectionName}}).Err()
}
