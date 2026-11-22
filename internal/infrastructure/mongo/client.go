package mongo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

const (
	defaultMongoHost             = "mongo"
	defaultMongoPort             = "27017"
	defaultMongoLocalThresholdMs = 15
	defaultMongoMaxPoolSize      = 100
	defaultMongoMinPoolSize      = 5
	minMongoMaxStalenessSec      = 90
)

var MongoClient *mongo.Client

// MongoConfig regroupe la configuration MongoDB résolue (Standalone ou Replica Set HA avec routage RTT).
type MongoConfig struct {
	URI            string
	ReplicaSet     string
	ReadPreference string
	LocalThreshold time.Duration
	MaxStaleness   time.Duration
	WriteConcern   string
	MaxPoolSize    uint64
	MinPoolSize    uint64
}

func parsePositiveIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func normalizeMongoHosts(rawHosts, defaultPort string) string {
	parts := strings.Split(rawHosts, ",")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		h := strings.TrimSpace(p)
		if h == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(h); err != nil {
			h = net.JoinHostPort(h, defaultPort)
		}
		cleaned = append(cleaned, h)
	}
	return strings.Join(cleaned, ",")
}

// ResolveMongoURI construit l'URI MongoDB de manière dynamique depuis les variables d'environnement.
// Priorité :
//  1. MONGO_URI complète (ex. Replica Set avec readPreference=nearest et tls=true)
//  2. MONGO_HOSTS (seed list CSV multi-hôtes Replica Set) ou MONGO_HOST / MONGO_PORT / MONGO_DB
//     avec credentials applicatifs (MONGO_APP_USER/MONGO_APP_PASSWORD en priorité, sinon MONGO_ROOT_*).
func ResolveMongoURI() string {
	if rawURI := strings.TrimSpace(os.Getenv("MONGO_URI")); rawURI != "" {
		return rawURI
	}

	port := strings.TrimSpace(os.Getenv("MONGO_PORT"))
	if port == "" {
		port = defaultMongoPort
	}

	var authority string
	if rawHosts := strings.TrimSpace(os.Getenv("MONGO_HOSTS")); rawHosts != "" {
		authority = normalizeMongoHosts(rawHosts, port)
	}
	if authority == "" {
		host := strings.TrimSpace(os.Getenv("MONGO_HOST"))
		if host == "" {
			host = defaultMongoHost
		}
		authority = net.JoinHostPort(host, port)
	}

	dbName := strings.TrimSpace(os.Getenv("MONGO_DB"))
	if dbName == "" {
		dbName = variables.MongoDatabaseName
	}

	user := strings.TrimSpace(os.Getenv("MONGO_APP_USER"))
	password := os.Getenv("MONGO_APP_PASSWORD")
	defaultAuthSource := dbName
	if user == "" {
		user = strings.TrimSpace(os.Getenv("MONGO_ROOT_USER"))
		password = os.Getenv("MONGO_ROOT_PASSWORD")
		defaultAuthSource = "admin"
	}

	authSource := strings.TrimSpace(os.Getenv("MONGO_AUTH_SOURCE"))
	if authSource == "" {
		authSource = defaultAuthSource
	}

	q := url.Values{}
	userPrefix := ""
	if user != "" {
		userPrefix = url.UserPassword(user, password).String() + "@"
		if authSource != "" {
			q.Set("authSource", authSource)
		}
	}

	if rs := strings.TrimSpace(os.Getenv("MONGO_REPLICA_SET")); rs != "" {
		q.Set("replicaSet", rs)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MONGO_TLS")), "true") {
		q.Set("tls", "true")
	}
	if rp := strings.TrimSpace(os.Getenv("MONGO_READ_PREFERENCE")); rp != "" {
		q.Set("readPreference", rp)
	}

	uri := fmt.Sprintf("mongodb://%s%s/%s", userPrefix, authority, dbName)
	if encoded := q.Encode(); encoded != "" {
		uri += "?" + encoded
	}
	return uri
}

// ResolveMongoConfig résout l'ensemble des paramètres HA, routage RTT, pool et tolérance aux pannes MongoDB.
func ResolveMongoConfig() MongoConfig {
	uri := ResolveMongoURI()
	rs := strings.TrimSpace(os.Getenv("MONGO_REPLICA_SET"))

	rp := strings.TrimSpace(os.Getenv("MONGO_READ_PREFERENCE"))
	if rp == "" && rs != "" && !strings.Contains(strings.ToLower(uri), "readpreference=") {
		rp = "secondaryPreferred"
	}

	localThresholdMs := parsePositiveIntEnv("MONGO_LOCAL_THRESHOLD_MS", defaultMongoLocalThresholdMs)

	var maxStaleness time.Duration
	if rawStale := strings.TrimSpace(os.Getenv("MONGO_MAX_STALENESS_SEC")); rawStale != "" {
		if sec, err := strconv.Atoi(rawStale); err == nil && sec > 0 {
			if sec < minMongoMaxStalenessSec {
				sec = minMongoMaxStalenessSec
			}
			maxStaleness = time.Duration(sec) * time.Second
		}
	}

	wc := strings.TrimSpace(os.Getenv("MONGO_WRITE_CONCERN"))
	if wc == "" && rs != "" {
		wc = "majority"
	}

	maxPool := uint64(parsePositiveIntEnv("MONGO_MAX_POOL_SIZE", defaultMongoMaxPoolSize))
	minPool := uint64(parsePositiveIntEnv("MONGO_MIN_POOL_SIZE", defaultMongoMinPoolSize))
	if minPool > maxPool {
		minPool = maxPool
	}

	return MongoConfig{
		URI:            uri,
		ReplicaSet:     rs,
		ReadPreference: rp,
		LocalThreshold: time.Duration(localThresholdMs) * time.Millisecond,
		MaxStaleness:   maxStaleness,
		WriteConcern:   wc,
		MaxPoolSize:    maxPool,
		MinPoolSize:    minPool,
	}
}

func resolveReadPref(mode string, maxStaleness time.Duration) *readpref.ReadPref {
	var rpOpts []readpref.Option
	if maxStaleness >= minMongoMaxStalenessSec*time.Second {
		rpOpts = append(rpOpts, readpref.WithMaxStaleness(maxStaleness))
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "primary":
		return readpref.Primary()
	case "primarypreferred":
		return readpref.PrimaryPreferred(rpOpts...)
	case "secondary":
		return readpref.Secondary(rpOpts...)
	case "secondarypreferred":
		return readpref.SecondaryPreferred(rpOpts...)
	case "nearest":
		return readpref.Nearest(rpOpts...)
	default:
		return nil
	}
}

// BuildMongoClientOptions construit et valide les options du driver MongoDB (routage RTT, retries, pool, write concern).
func BuildMongoClientOptions(cfg MongoConfig) (*options.ClientOptions, error) {
	opts := options.Client().
		ApplyURI(cfg.URI).
		SetLocalThreshold(cfg.LocalThreshold).
		SetRetryWrites(true).
		SetRetryReads(true).
		SetMaxPoolSize(cfg.MaxPoolSize).
		SetMinPoolSize(cfg.MinPoolSize)

	if cfg.ReplicaSet != "" {
		opts.SetReplicaSet(cfg.ReplicaSet)
	}
	if rp := resolveReadPref(cfg.ReadPreference, cfg.MaxStaleness); rp != nil {
		opts.SetReadPreference(rp)
	}
	if strings.EqualFold(cfg.WriteConcern, "majority") {
		opts.SetWriteConcern(writeconcern.Majority())
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return opts, nil
}

// RedactMongoURI masque tout mot de passe présent dans une URI MongoDB (mono-hôte ou multi-hôtes) avant journalisation.
func RedactMongoURI(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" {
		return "<redacted-mongo-uri>"
	}
	return fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, parsed.Path)
}

func InitMongo() {
	cfg := ResolveMongoConfig()
	safeTarget := RedactMongoURI(cfg.URI)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions, err := BuildMongoClientOptions(cfg)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Str("mongo_target", safeTarget).Msg("Configuration MongoDB invalide")
	}

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Str("mongo_target", safeTarget).Msg("Impossible de créer le client Mongo")
	}

	// PrimaryPreferred tolère une brève élection Raft (<2s) au démarrage d'un pod sans crash en cascade
	err = client.Ping(ctx, readpref.PrimaryPreferred())
	if err != nil {
		numan_log.Fatal(ctx).Err(err).Str("mongo_target", safeTarget).Msg("Impossible de ping Mongo")
	}

	MongoClient = client
	numan_log.Info(ctx).
		Str("mongo_target", safeTarget).
		Str("replica_set", cfg.ReplicaSet).
		Str("read_preference", cfg.ReadPreference).
		Int64("local_threshold_ms", cfg.LocalThreshold.Milliseconds()).
		Str("write_concern", cfg.WriteConcern).
		Int("max_pool_size", int(cfg.MaxPoolSize)).
		Int("min_pool_size", int(cfg.MinPoolSize)).
		Msg("Connecté à MongoDB avec succès !")
}

// CloseMongo ferme proprement la connexion MongoDB lors d'un arrêt gracieux.
func CloseMongo(ctx context.Context) error {
	if MongoClient == nil {
		return nil
	}
	return MongoClient.Disconnect(ctx)
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
	userRecoSkips := db.Collection(variables.MongoCollUserRecoSkips)

	// 2. Index de recherche vitaux (comme avant)
	_, err1 := users.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "username", Value: 1}, {Key: "email", Value: 1}, {Key: "phone", Value: 1}}}})
	_, err2 := userSettings.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "user_id", Value: 1}}}})
	_, err3 := sessions.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "firebase_installation_id", Value: 1}}}})
	_, err4 := relations.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "primary_id", Value: 1}, {Key: "secondary_id", Value: 1}}}})
	_, err5 := posts.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "hashtags", Value: 1}, {Key: "identifiers", Value: 1}, {Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "lang", Value: 1}, {Key: "visibility", Value: 1}, {Key: "created_at", Value: -1}}},
	})
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

	// A2. User Recommendation Skips : Index composé unique (user_id, skipped_user_id) + TTL strict (7 jours = 604800s) sur created_at
	_, err14 := userRecoSkips.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "skipped_user_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "created_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(int32(variables.UserRecoSkipTTLSeconds)),
		},
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

	articles := db.Collection(variables.MongoCollArticles)
	_, _ = articles.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "visibility", Value: 1}, {Key: "created_at", Value: -1}}},
	})

	slidingCollections := []*mongo.Collection{
		users, userSettings, sessions, relations, posts, comments,
		likes, media, conversations, members, messages, saved, userPasskeys, articles,
	}

	var ttlErrs []error
	for _, coll := range slidingCollections {
		_, err := coll.Indexes().CreateOne(ctx, slidingTTLIndex)
		if err != nil {
			ttlErrs = append(ttlErrs, err)
		}
	}

	return errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10, err11, err12, err13, err14, errors.Join(ttlErrs...))
}
