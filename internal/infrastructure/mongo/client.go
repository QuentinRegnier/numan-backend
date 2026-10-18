package mongo

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var MongoClient *mongo.Client

func InitMongo() {
	// 1. On récupère l'URI depuis le .env
	user := os.Getenv("MONGO_ROOT_USER")
	password := os.Getenv("MONGO_ROOT_PASSWORD")
	uri := "mongodb://" + user + ":" + password + "@mongo:27017"

	// SÉCURITÉ : Si vide, on met une valeur par défaut, MAIS on prévient
	if uri == "" {
		numan_log.Warn(context.Background()).Msg("MONGO_URI vide, fallback sur localhost (ça plantera dans Docker !)")
		uri = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. On se connecte
	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Msg("Impossible de créer le client Mongo")
	}

	// 3. Ping pour vérifier que ça marche VRAIMENT
	err = client.Ping(ctx, nil)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Str("uri", uri).Msg("Impossible de ping Mongo")
	}

	MongoClient = client
	numan_log.Info(ctx).Msg("Connecté à MongoDB avec succès !")
}

func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	// 1. Déclaration des collections
	users := db.Collection("auth.users")
	userSettings := db.Collection("auth.user_settings")
	sessions := db.Collection("auth.sessions")
	relations := db.Collection("auth.relations")
	posts := db.Collection("content.posts")
	comments := db.Collection("content.comments")
	likes := db.Collection("content.likes")
	media := db.Collection("content.media")
	conversations := db.Collection("messaging.conversations")
	members := db.Collection("messaging.conversation_members")
	messages := db.Collection("messaging.messages")
	saved := db.Collection("content.saved")
	notifications := db.Collection("activity.notifications")

	// 2. Index de recherche vitaux (comme avant)
	_, err1 := users.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "username", Value: 1}, {Key: "email", Value: 1}, {Key: "phone", Value: 1}}}})
	_, err2 := userSettings.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "user_id", Value: 1}}}})
	_, err3 := sessions.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "firebase_installation_id", Value: 1}}}})
	_, err4 := relations.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "primary_id", Value: 1}, {Key: "secondary_id", Value: 1}}}})
	_, err5 := posts.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "hashtags", Value: 1}, {Key: "identifiers", Value: 1}, {Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}}})
	_, err6 := comments.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "post_id", Value: 1}, {Key: "created_at", Value: -1}}}})
	_, err7 := likes.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "target_type", Value: 1}, {Key: "target_id", Value: 1}, {Key: "user_id", Value: 1}}}})
	_, err8 := media.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "created_at", Value: -1}}}})
	_, err9 := conversations.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "last_message_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "state", Value: 1}}}})
	_, err10 := members.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "user_id", Value: 1}}}})
	_, err11 := messages.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "created_at", Value: -1}}}})

	// Index pagination pour les notifications
	_, err12 := notifications.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}}})

	// 3. APPLICATION DES INDEX TTL (LA PURGE AUTOMATIQUE)
	ttl30Days := int32(30 * 24 * 60 * 60)

	// A. Notifications : Expiration STRICTE basée sur la date de création
	_, err13 := notifications.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(ttl30Days),
	})

	// B. Tout le reste du L2 : Expiration GLISSANTE basée sur le last_use
	slidingTTLIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "last_use", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(ttl30Days),
	}

	userPasskeys := db.Collection("auth.user_passkeys")
	_, _ = userPasskeys.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "credential_id", Value: 1}}, Options: options.Index().SetUnique(true)},
	})

	slidingCollections := []*mongo.Collection{
		users, userSettings, sessions, relations, posts, comments,
		likes, media, conversations, members, messages, saved, userPasskeys,
	}

	var ttlErrs []error
	for _, coll := range slidingCollections {
		_, err := coll.Indexes().CreateOne(ctx, slidingTTLIndex)
		if err != nil {
			ttlErrs = append(ttlErrs, err)
		}
	}

	return errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10, err11, err12, err13, errors.Join(ttlErrs...))
}
