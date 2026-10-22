package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/schemas"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	mongogo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
)

// ---------------- Initialisation ----------------
// declarations globales
var (
	Users            *Collection
	UserSettings     *Collection
	Sessions         *Collection
	Relations        *Collection
	Posts            *Collection
	Comments         *Collection
	Likes            *Collection
	Media            *Collection
	Conversations    *Collection
	Members          *Collection
	Messages         *Collection
	MessageReactions *Collection
	Saved            *Collection
	Passkeys         *Collection
	Notifications    *Collection
)

// InitCacheDatabase initialise la structure logique de MongoDB pour les caches (Warm Storage L2)
func InitCacheDatabase() {
	// Initialiser les collections

	schemaUsers := schemas.UsersSchema
	schemaUserSettings := schemas.UserSettingsSchema
	schemaSessions := schemas.SessionsSchema
	schemaRelations := schemas.RelationsSchema
	schemaPosts := schemas.PostsSchema
	schemaComments := schemas.CommentsSchema
	schemaLikes := schemas.LikesSchema
	schemaMedia := schemas.MediaSchema
	schemaConversations := schemas.ConversationsSchema
	schemaMembers := schemas.MembersSchema
	schemaMessages := schemas.MessagesSchema
	schemaMessageReactions := schemas.MessageReactionsSchema
	schemaSaved := schemas.SavedSchema
	schemaPasskeys := schemas.PasskeysSchema
	schemaNotifications := schemas.NotificationsSchema

	// variables globales
	Users = newMongoCollection("numan_mongo", "auth.users", schemaUsers)
	UserSettings = newMongoCollection("numan_mongo", "auth.user_settings", schemaUserSettings)
	Sessions = newMongoCollection("numan_mongo", "auth.sessions", schemaSessions)
	Relations = newMongoCollection("numan_mongo", "auth.relations", schemaRelations)
	Posts = newMongoCollection("numan_mongo", "content.posts", schemaPosts)
	Comments = newMongoCollection("numan_mongo", "content.comments", schemaComments)
	Likes = newMongoCollection("numan_mongo", "content.likes", schemaLikes)
	Media = newMongoCollection("numan_mongo", "content.media", schemaMedia)
	Conversations = newMongoCollection("numan_mongo", "messaging.conversations", schemaConversations)
	Members = newMongoCollection("numan_mongo", "messaging.members", schemaMembers)
	Messages = newMongoCollection("numan_mongo", "messaging.messages", schemaMessages)
	MessageReactions = newMongoCollection("numan_mongo", "messaging.message_reactions", schemaMessageReactions)
	Saved = newMongoCollection("numan_mongo", "content.saved", schemaSaved)
	Passkeys = newMongoCollection("numan_mongo", "auth.user_passkeys", schemaPasskeys)
	Notifications = newMongoCollection("numan_mongo", "activity.notifications", schemaNotifications)
}

// ---------------- Collection et schéma ----------------

type Collection struct {
	Name   string
	Schema map[string]reflect.Kind
	DB     *mongo.Database
}

// newMongoCollection crée une collection Mongo avec un schéma
func newMongoCollection(dbName, name string, schema map[string]reflect.Kind) *Collection {
	var db *mongo.Database
	if mongogo.MongoClient != nil {
		db = mongogo.MongoClient.Database(dbName)
	}
	return &Collection{
		Name:   name,
		Schema: schema,
		DB:     db,
	}
}

// validate vérifie les types.
// partial = true : permet de ne vérifier QUE les champs présents (pour update)
func (c *Collection) validate(ctx context.Context, obj map[string]any, partial bool) error {
	if c == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection is not initialized"),
		)
	}

	if !partial {
		for field := range c.Schema {
			if _, ok := obj[field]; !ok {
				return numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeDatabaseError,
					"Échec de la validation de la structure de données.",
					fmt.Errorf("champ requis manquant dans le document MongoDB (collection %s) : %s", c.Name, field),
				)
			}
		}
	}

	for field, val := range obj {
		expectedKind, known := c.Schema[field]
		if !known {
			continue
		}
		if reflect.TypeOf(val).Kind() != expectedKind {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Incohérence de typage détectée.",
				fmt.Errorf("champ %s attendu comme %s, reçu %s (collection %s)", field, expectedKind.String(), reflect.TypeOf(val).Kind().String(), c.Name),
			)
		}
	}
	return nil
}

// Set insère ou met à jour un objet dans la collection
func (c *Collection) Set(obj map[string]any) error {
	if c == nil || c.DB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection or database is not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// false = on veut valider que TOUS les champs sont là
	if err := c.validate(ctx, obj, false); err != nil {
		return err
	}

	// AUTOMATISATION DU SLIDING TTL
	obj["last_use"] = time.Now().UTC()

	collection := c.DB.Collection(c.Name)

	// upsert (si id existe déjà, on remplace)
	filter := bson.M{"id": obj["id"]}
	update := bson.M{"$set": obj}
	opts := options.Update().SetUpsert(true)

	_, err := collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de sauvegarder l'entité (Storage L2).",
			fmt.Errorf("échec UpdateOne sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}

	return nil
}

// Get récupère les objets correspondant au filtre avec une projection optionnelle
func (c *Collection) Get(filter map[string]any, projection map[string]any) ([]map[string]any, error) {
	if c == nil || c.DB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection or database is not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := c.DB.Collection(c.Name)

	opts := options.Find()
	if projection != nil {
		opts.SetProjection(projection)
	}

	cur, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de lire les données (Storage L2).",
			fmt.Errorf("échec Find sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}
	defer cur.Close(ctx) // Erreur de fermeture ignorée pour rester focalisé sur l'action principale

	var results []map[string]any
	if err := cur.All(ctx, &results); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'extraire les résultats de la requête (Storage L2).",
			fmt.Errorf("échec cur.All sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}

	if results == nil {
		results = []map[string]any{}
	}

	return results, nil
}

// Delete supprime les objets correspondant au filtre
func (c *Collection) Delete(filter map[string]any) error {
	if c == nil || c.DB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection or database is not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := c.DB.Collection(c.Name)
	_, err := collection.DeleteMany(ctx, filter)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de purger les données (Storage L2).",
			fmt.Errorf("échec DeleteMany sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}

	return nil
}

// update met à jour les éléments correspondant au filtre avec les nouvelles valeurs fournies dans update
func (c *Collection) update(filter map[string]any, update map[string]any) error {
	if c == nil || c.DB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection or database is not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// true = on valide seulement les champs qu'on veut mettre à jour
	if err := c.validate(ctx, update, true); err != nil {
		return err
	}

	// AUTOMATISATION DU SLIDING TTL (Y compris pour les Soft Deletes)
	update["last_use"] = time.Now().UTC()

	collection := c.DB.Collection(c.Name)
	_, err := collection.UpdateMany(ctx, filter, bson.M{"$set": update})
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de mettre à jour les données (Storage L2).",
			fmt.Errorf("échec UpdateMany sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}

	return nil
}

// GetPaginated récupère les objets avec pagination (Skip/Limit) et tri (Sort)
func (c *Collection) GetPaginated(filter map[string]any, sort any, skip int64, limit int64) ([]map[string]any, error) {
	if c == nil || c.DB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection or database is not initialized"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := c.DB.Collection(c.Name)

	opts := options.Find()
	if sort != nil {
		opts.SetSort(sort)
	}
	opts.SetSkip(skip)
	opts.SetLimit(limit)

	cur, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de lire les données paginées (Storage L2).",
			fmt.Errorf("échec Find paginé sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}
	defer cur.Close(ctx)

	var results []map[string]any
	if err := cur.All(ctx, &results); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'extraire les résultats paginés de la requête (Storage L2).",
			fmt.Errorf("échec cur.All paginé sur MongoDB (collection %s) : %w", c.Name, err),
		)
	}

	if results == nil {
		results = []map[string]any{}
	}

	return results, nil
}
