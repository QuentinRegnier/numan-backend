package cache_service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
	"go.mongodb.org/mongo-driver/x/mongo/driver/wiremessage"
)

// verifiedRootMongoServer implements a MongoDB wire-protocol test double for
// package cache_service using the exact go.mongodb.org/mongo-driver v1.17.4
// wiremessage and bsoncore APIs without deprecated OpQuery or ReadQuery* calls.
type verifiedRootMongoServer struct {
	listener net.Listener
	mu       sync.RWMutex
	docs     map[string][]bson.M
	failColl map[string]bool
	upserted map[string][]bson.M
}

func setupVerifiedRootMongo(t *testing.T) *verifiedRootMongoServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start verified mongo listener: %v", err)
	}

	srv := &verifiedRootMongoServer{
		listener: ln,
		docs:     make(map[string][]bson.M),
		failColl: make(map[string]bool),
		upserted: make(map[string][]bson.M),
	}

	go srv.acceptLoop()

	uri := fmt.Sprintf("mongodb://%s/?directConnection=true&serverSelectionTimeoutMS=2000", ln.Addr().String())
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		_ = ln.Close()
		t.Fatalf("failed to connect verified mongo client: %v", err)
	}

	prevClient := infraMongo.MongoClient
	infraMongo.MongoClient = client
	repoMongo.InitCacheDatabase()

	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
		_ = ln.Close()
		infraMongo.MongoClient = prevClient
	})

	return srv
}

func (m *verifiedRootMongoServer) setDocs(coll string, docs ...bson.M) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[coll] = docs
}

func (m *verifiedRootMongoServer) setFailCollection(coll string, fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failColl[coll] = fail
}

func (m *verifiedRootMongoServer) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		go m.handleConn(conn)
	}
}

func (m *verifiedRootMongoServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	for {
		header := make([]byte, 16)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		length, reqID, _, opCode, _, ok := wiremessage.ReadHeader(header)
		if !ok || length < 16 {
			return
		}
		body := make([]byte, length-16)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}

		var cmdDoc bson.Raw
		const legacyOpQueryCode = wiremessage.OpCode(2004)

		switch opCode {
		case legacyOpQueryCode:
			if len(body) < 12 {
				return
			}
			pos := 4
			for pos < len(body) && body[pos] != 0 {
				pos++
			}
			pos++
			if pos+8 > len(body) {
				return
			}
			pos += 8
			cmdDoc = bson.Raw(body[pos:])

		case wiremessage.OpMsg:
			_, rem, okFlags := wiremessage.ReadMsgFlags(body)
			if !okFlags {
				return
			}
			for len(rem) > 0 {
				var st wiremessage.SectionType
				var okSec bool
				st, rem, okSec = wiremessage.ReadMsgSectionType(rem)
				if !okSec {
					break
				}
				switch st {
				case wiremessage.SingleDocument:
					var docBytes []byte
					docBytes, rem, okSec = wiremessage.ReadMsgSectionSingleDocument(rem)
					if okSec && len(cmdDoc) == 0 {
						cmdDoc = bson.Raw(docBytes)
					}
				case wiremessage.DocumentSequence:
					var seqDocs []bsoncore.Document
					_, seqDocs, rem, okSec = wiremessage.ReadMsgSectionDocumentSequence(rem)
					if okSec && len(seqDocs) > 0 {
						var docsBytes [][]byte
						for _, doc := range seqDocs {
							docsBytes = append(docsBytes, []byte(doc))
						}
						m.recordUpdateDocs(cmdDoc, docsBytes)
					}
				}
			}
		default:
			return
		}

		replyDoc := m.buildReply(cmdDoc)
		replyBytes, _ := bson.Marshal(replyDoc)

		if opCode == legacyOpQueryCode {
			var dst []byte
			idx, dst := wiremessage.AppendHeaderStart(dst, 1, reqID, wiremessage.OpReply)
			dst = wiremessage.AppendReplyFlags(dst, 0)
			dst = wiremessage.AppendReplyCursorID(dst, 0)
			dst = wiremessage.AppendReplyStartingFrom(dst, 0)
			dst = wiremessage.AppendReplyNumberReturned(dst, 1)
			dst = append(dst, replyBytes...)
			dst = bsoncore.UpdateLength(dst, idx, int32(len(dst)))
			_, _ = conn.Write(dst)
		} else {
			var dst []byte
			idx, dst := wiremessage.AppendHeaderStart(dst, 1, reqID, wiremessage.OpMsg)
			dst = wiremessage.AppendMsgFlags(dst, 0)
			dst = wiremessage.AppendMsgSectionType(dst, wiremessage.SingleDocument)
			dst = append(dst, replyBytes...)
			dst = bsoncore.UpdateLength(dst, idx, int32(len(dst)))
			_, _ = conn.Write(dst)
		}
	}
}

func (m *verifiedRootMongoServer) recordUpdateDocs(cmdDoc bson.Raw, docsBytes [][]byte) {
	var parsed bson.M
	if err := bson.Unmarshal(cmdDoc, &parsed); err != nil {
		return
	}
	collName, ok := parsed["update"].(string)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range docsBytes {
		var u bson.M
		if bson.Unmarshal(b, &u) == nil {
			m.upserted[collName] = append(m.upserted[collName], u)
		}
	}
}

func (m *verifiedRootMongoServer) buildReply(cmdDoc bson.Raw) bson.D {
	elems, err := cmdDoc.Elements()
	if err != nil || len(elems) == 0 {
		return bson.D{{Key: "ok", Value: 1.0}}
	}
	firstKey := strings.ToLower(elems[0].Key())

	switch firstKey {
	case "ismaster", "hello":
		return bson.D{
			{Key: "ismaster", Value: true},
			{Key: "isWritablePrimary", Value: true},
			{Key: "maxBsonObjectSize", Value: int32(16777216)},
			{Key: "maxMessageSizeBytes", Value: int32(48000000)},
			{Key: "maxWriteBatchSize", Value: int32(100000)},
			{Key: "localTime", Value: time.Now()},
			{Key: "minWireVersion", Value: int32(0)},
			{Key: "maxWireVersion", Value: int32(17)},
			{Key: "ok", Value: 1.0},
		}
	case "find":
		collName, _ := elems[0].Value().StringValueOK()
		m.mu.RLock()
		shouldFail := m.failColl[collName]
		docs := append([]bson.M(nil), m.docs[collName]...)
		m.mu.RUnlock()

		if shouldFail {
			return bson.D{
				{Key: "ok", Value: 0.0},
				{Key: "errmsg", Value: "simulated mongo find error"},
				{Key: "code", Value: int32(1)},
			}
		}

		batch := make(bson.A, 0, len(docs))
		for _, d := range docs {
			batch = append(batch, d)
		}
		return bson.D{
			{Key: "cursor", Value: bson.D{
				{Key: "firstBatch", Value: batch},
				{Key: "id", Value: int64(0)},
				{Key: "ns", Value: "numan_mongo." + collName},
			}},
			{Key: "ok", Value: 1.0},
		}
	case "update":
		collName, _ := elems[0].Value().StringValueOK()
		m.mu.RLock()
		shouldFail := m.failColl[collName]
		m.mu.RUnlock()
		if shouldFail {
			return bson.D{
				{Key: "ok", Value: 0.0},
				{Key: "errmsg", Value: "simulated mongo update error"},
				{Key: "code", Value: int32(1)},
			}
		}
		return bson.D{
			{Key: "n", Value: int32(1)},
			{Key: "nModified", Value: int32(1)},
			{Key: "ok", Value: 1.0},
		}
	default:
		return bson.D{{Key: "ok", Value: 1.0}}
	}
}

func TestMostCacheService_VerifiedAPIsAndInt64Offsets(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupVerifiedRootMongo(t)
	ctx := context.Background()

	t.Run("EvaluatePostAfterLike and EvaluatePostAfterView register active tags via GetTagFromKeyword and update ZSETs", func(t *testing.T) {
		if err := redis.Tags.SAdd(ctx, "active", "golang", "testing"); err != nil {
			t.Fatalf("redis.Tags.SAdd() unexpected error: %v", err)
		}
		if slug, ok := service.GetTagFromKeyword(ctx, "  #GoLang "); !ok || slug != "golang" {
			t.Fatalf("service.GetTagFromKeyword() = (%q, %v), want (\"golang\", true)", slug, ok)
		}

		nowMs := time.Now().Add(-10 * time.Minute).UnixMilli()
		post := post_models.PostPayload{
			ID:               101,
			UserID:           10,
			Content:          "Hello world #golang",
			Hashtags:         []string{"golang"},
			IndirectHashtags: []string{"testing"},
			Identifiers:      []int64{},
			MediaIDs:         []int64{1},
			Visibility:       1,
			PriorityLevel:    2,
			Location:         "Lyon",
			LikeCount:        25,
			CommentCount:     5,
			ViewCount:        200,
			ReportCount:      0,
			HasMedia:         true,
			Vector:           []float32{0.1, 0.9},
			VectorVersion:    1,
			CreatedAt:        nowMs,
			UpdatedAt:        nowMs,
		}

		EvaluatePostAfterLike(ctx, post)
		EvaluatePostAfterView(ctx, post)

		likesList, err := redis.ZRange(ctx, variables.RedisKeyStrictLikes, 0, -1)
		if err != nil || !reflect.DeepEqual(likesList, []string{"101"}) {
			t.Errorf("StrictLikes ZSET = %v (err=%v), want [\"101\"]", likesList, err)
		}

		viewsList, err := redis.ZRange(ctx, variables.RedisKeyStrictViews, 0, -1)
		if err != nil || !reflect.DeepEqual(viewsList, []string{"101"}) {
			t.Errorf("StrictViews ZSET = %v (err=%v), want [\"101\"]", viewsList, err)
		}

		recentList, err := redis.ZRange(ctx, variables.RedisKeyStrictRecent, 0, -1)
		if err != nil || !reflect.DeepEqual(recentList, []string{"101"}) {
			t.Errorf("StrictRecent ZSET = %v (err=%v), want [\"101\"]", recentList, err)
		}

		leaderboard, err := redis.HashtagLeaderboard.ZRevRange(ctx, "global", 0, -1)
		if err != nil || len(leaderboard) < 2 {
			t.Errorf("HashtagLeaderboard = %v (err=%v), want >= 2 tags", leaderboard, err)
		}
	})

	t.Run("GetRankedPosts and GetTagPosts use explicit int64 offsets for L1, L2 MongoDB, and L3 Postgres", func(t *testing.T) {
		nowMs := int64(1700000500000)
		mongoSrv.setDocs("content.posts", bson.M{
			"id":         int64(303),
			"user_id":    int64(30),
			"content":    "Cold Ranked Post L2",
			"hashtags":   bson.A{"go"},
			"visibility": int32(1),
			"like_count": int32(500),
			"view_count": int32(2500),
			"created_at": nowMs,
			"updated_at": nowMs,
		})

		strictOffset := int64(variables.MaxStrictElements)
		rankedL2, err := GetRankedPosts(ctx, "strict:likes", strictOffset, 10)
		if err != nil {
			t.Fatalf("GetRankedPosts(L2) error: %v", err)
		}
		if len(rankedL2) != 1 || rankedL2[0].ID != 303 || rankedL2[0].LikeCount != 500 {
			t.Fatalf("GetRankedPosts(L2) = %+v, want ID=303 LikeCount=500", rankedL2)
		}

		tagOffset := int64(variables.MaxTagElements)
		tagPostsL2, err := GetTagPosts(ctx, "go", tagOffset, 10)
		if err != nil {
			t.Fatalf("GetTagPosts(L2) error: %v", err)
		}
		if len(tagPostsL2) != 1 || tagPostsL2[0].ID != 303 {
			t.Fatalf("GetTagPosts(L2) = %+v, want ID=303", tagPostsL2)
		}

		// Force L2 failure -> L3 Postgres fallback
		mongoSrv.setFailCollection("content.posts", true)
		defer mongoSrv.setFailCollection("content.posts", false)

		hashtagsVal, _ := pq.Array([]string{"go"}).Value()
		indirectVal, _ := pq.Array([]string{}).Value()
		identVal, _ := pq.Array([]int64{}).Value()
		mediaVal, _ := pq.Array([]int64{}).Value()
		vecVal, _ := pq.Array([]float32{0.5, 0.5}).Value()

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			if strings.Contains(query, "func_load_post_ids_by_tag_paginated") || strings.Contains(query, "FROM content.") {
				return fakeSQLResponse{
					cols: []string{"id"},
					rows: [][]driver.Value{{int64(503)}},
				}
			}
			return fakeSQLResponse{
				cols: postColumns22(),
				rows: [][]driver.Value{{
					int64(503), int64(10), "Tag L3 Postgres", hashtagsVal, indirectVal,
					identVal, mediaVal, int64(1), int64(0), "Paris",
					nowMs, nowMs, int64(12), int64(3), int64(90),
					int64(0), false, vecVal, int64(1), float64(10.5),
					float64(50.25), int64(4),
				}},
			}
		})

		tagPostsL3, err := GetTagPosts(ctx, "go", tagOffset, 10)
		if err != nil {
			t.Fatalf("GetTagPosts(L3) error: %v", err)
		}
		if len(tagPostsL3) != 1 || tagPostsL3[0].ID != 503 || tagPostsL3[0].Location != "Paris" {
			t.Fatalf("GetTagPosts(L3) = %+v, want ID=503 Location=\"Paris\"", tagPostsL3)
		}
	})
}

func TestSeedingService_VerifiedPrimitivesAndRelationIndexes(t *testing.T) {
	setupTestRedis(t)
	setupVerifiedRootMongo(t)
	ctx := context.Background()

	t.Run("SeedMostCache disables maintenance mode readable via SystemStatus.Client.Get", func(t *testing.T) {
		hashtagsVal, _ := pq.Array([]string{"golang"}).Value()
		indirectVal, _ := pq.Array([]string{}).Value()
		identVal, _ := pq.Array([]int64{}).Value()
		mediaVal, _ := pq.Array([]int64{}).Value()
		vecVal, _ := pq.Array([]float32{}).Value()
		nowMs := time.Now().UnixMilli()

		postRow := []driver.Value{
			int64(1001), int64(10), "Seeded Post", hashtagsVal, indirectVal,
			identVal, mediaVal, int64(1), int64(1), "Paris",
			nowMs, nowMs, int64(42), int64(7), int64(500),
			int64(0), false, vecVal, int64(1), float64(0),
			float64(0), int64(0),
		}

		paginatedCalls := 0
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			if strings.Contains(query, "func_load_all_tags") {
				return fakeSQLResponse{
					cols: []string{"tag"},
					rows: [][]driver.Value{{"golang"}, {"architecture"}},
				}
			}
			if strings.Contains(query, "func_load_posts_paginated") {
				paginatedCalls++
				if paginatedCalls == 1 {
					return fakeSQLResponse{cols: postColumns22(), rows: [][]driver.Value{postRow}}
				}
				return fakeSQLResponse{cols: postColumns22(), rows: [][]driver.Value{}}
			}
			return fakeSQLResponse{cols: postColumns22(), rows: [][]driver.Value{postRow}}
		})

		_ = redis.ZAddWithCap(ctx, "most_cache:trend:test", 99.5, int64(1001), 100)

		if err := SeedMostCache(ctx); err != nil {
			t.Fatalf("SeedMostCache() unexpected error: %v", err)
		}

		elitePost, err := object_cache_service.GetPostFromObjectCache(ctx, 1001)
		if err != nil || elitePost.ID != 1001 || elitePost.LikeCount != 42 {
			t.Errorf("GetPostFromObjectCache(1001) = (%+v, %v), want ID=1001 LikeCount=42", elitePost, err)
		}

		// Verify maintenance primitive using the real Redis client & key builder on SystemStatus
		status, err := redis.SystemStatus.Client.Get(ctx, redis.SystemStatus.Key("maintenance")).Result()
		if err != nil || status != "off" {
			t.Errorf("SystemStatus maintenance = (%q, %v), want (\"off\", nil)", status, err)
		}

		// Verify SQL failure in FuncLoadPostsPaginated returns 500 AppError
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			if strings.Contains(query, "func_load_posts_paginated") {
				return fakeSQLResponse{err: errors.New("sql failure")}
			}
			return fakeSQLResponse{cols: []string{"tag"}, rows: nil}
		})
		errSeedFail := SeedMostCache(ctx)
		var appErr *numan_error.AppError
		if !errors.As(errSeedFail, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SeedMostCache() on SQL failure = %v, want 500 AppError", errSeedFail)
		}
	})

	t.Run("SeedSpeedCache populates UserLite value struct and relation indexes with variables.RelationStateFollow", func(t *testing.T) {
		setupTestRedis(t)
		nowTime := time.Now().UTC().Truncate(time.Second)

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			switch {
			case strings.Contains(query, "func_load_users_paginated"):
				return fakeSQLResponse{
					cols: []string{"id", "username", "first_name", "last_name", "profile_picture_id", "bio", "grade", "conversation_permission", "add_group_permission"},
					rows: [][]driver.Value{
						{int64(10), "alice", "Alice", "Smith", int64(1), "Bio", int64(1), int64(0), int64(0)},
					},
				}
			case strings.Contains(query, "func_load_relations_paginated"):
				return fakeSQLResponse{
					cols: []string{"caller_id", "target_id", "state", "created_at"},
					rows: [][]driver.Value{
						{int64(10), int64(20), int64(variables.RelationStateFollow), nowTime},
					},
				}
			case strings.Contains(query, "func_load_active_communities"):
				return fakeSQLResponse{
					cols: []string{"id", "name", "description", "avatar_id", "member_count"},
					rows: [][]driver.Value{
						{int64(500), "Go Devs", "Golang Community", int64(9), int64(42)},
					},
				}
			case strings.Contains(query, "func_load_active_conversations"):
				return fakeSQLResponse{
					cols: []string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link"},
					rows: [][]driver.Value{
						{int64(600), int64(0), "DM", "", int64(0), int64(888), `{}`, `[]`},
					},
				}
			default:
				return fakeSQLResponse{
					cols: []string{"conversation_id", "user_id", "role", "settings", "unread_count", "frozen_message_id", "last_read_message_id", "last_message_id", "joined_at"},
					rows: [][]driver.Value{
						{int64(600), int64(10), int64(0), `{"pinned":-1,"is_muted":1}`, int64(2), int64(0), int64(888), int64(888), nowTime},
					},
				}
			}
		})

		if err := SeedSpeedCache(ctx); err != nil {
			t.Fatalf("SeedSpeedCache() unexpected error: %v", err)
		}

		// GetUserLite returns a value struct (lite_models.UserLiteRequest), never compare to nil
		userLite, err := GetUserLite(ctx, 10)
		if err != nil {
			t.Fatalf("GetUserLite(10) unexpected error: %v", err)
		}
		if userLite.ID != 10 || userLite.Username != "alice" || userLite.FirstName != "Alice" {
			t.Errorf("GetUserLite(10) = %+v, want ID=10 Username=\"alice\" FirstName=\"Alice\"", userLite)
		}

		// Caller 10 follows Target 20 -> Target 20's incoming followers index contains [10]
		followersOf20, err := GetSpeedRelationsIndex(ctx, 20)
		if err != nil || !reflect.DeepEqual(followersOf20, []int64{10}) {
			t.Errorf("GetSpeedRelationsIndex(20) = (%v, %v), want ([10], nil)", followersOf20, err)
		}
		if gotState := RelationValue(ctx, 20, 10); gotState != variables.RelationStateFollow {
			t.Errorf("RelationValue(target=20, caller=10) = %d, want %d", gotState, variables.RelationStateFollow)
		}
		if count := GetFollowerCount(ctx, 20); count != 1 {
			t.Errorf("GetFollowerCount(20) = %d, want 1", count)
		}
	})
}

func TestConversationSpeedCache_IntegerIsMutedAndMemberSettingsFidelity(t *testing.T) {
	setupTestRedis(t)
	ctx := context.Background()

	t.Run("MemberSettings JSON unmarshaling requires integer is_muted and preserves all fields", func(t *testing.T) {
		rawValidJSON := []byte(`{"is_muted":1,"mute_expire_at":1700000999000,"pinned":2,"media_auto_download":true,"restricted_until":1700000888000}`)
		var parsed member_models.MemberSettings
		if err := json.Unmarshal(rawValidJSON, &parsed); err != nil {
			t.Fatalf("json.Unmarshal(valid integer is_muted) unexpected error: %v", err)
		}
		if parsed.IsMuted != 1 || parsed.MuteExpireAt != 1700000999000 || parsed.Pinned != 2 || !parsed.MediaAutoDownload || parsed.RestrictedUntil != 1700000888000 {
			t.Errorf("parsed MemberSettings = %+v, want all fields preserved", parsed)
		}

		// Verify that a boolean "is_muted":true fails JSON unmarshaling into MemberSettings (int)
		rawInvalidBoolJSON := []byte(`{"pinned":0,"is_muted":true}`)
		var invalidParsed member_models.MemberSettings
		if err := json.Unmarshal(rawInvalidBoolJSON, &invalidParsed); err == nil {
			t.Error("expected json.Unmarshal error when is_muted is boolean true instead of int, got nil")
		}
	})

	t.Run("seedMessagingSpeedCache and L3 fallback preserve integer IsMuted on MemberLiteRequest", func(t *testing.T) {
		joinedTime := time.Now().UTC().Truncate(time.Second)
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			if strings.Contains(query, "func_load_active_conversations") {
				return fakeSQLResponse{
					cols: []string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link"},
					rows: [][]driver.Value{
						{int64(301), int64(1), "General", "Desc", int64(5), int64(1001), `{"write_permission":0,"send_media_permission":true}`, `["https://example.com"]`},
					},
				}
			}
			return fakeSQLResponse{
				cols: []string{"conversation_id", "user_id", "role", "settings", "unread_count", "frozen_message_id", "last_read_message_id", "last_message_id", "joined_at"},
				rows: [][]driver.Value{
					{int64(301), int64(44), int64(2), `{"pinned":0,"is_muted":1,"mute_expire_at":1700000123000,"restricted_until":0}`, int64(3), int64(0), int64(1000), int64(1001), joinedTime},
				},
			}
		})

		if err := seedMessagingSpeedCache(ctx); err != nil {
			t.Fatalf("seedMessagingSpeedCache() unexpected error: %v", err)
		}

		var conv lite_models.ConvLiteRequest
		if err := redis.ConvMeta.GetObject(ctx, int64(301), &conv); err != nil {
			t.Fatalf("redis.ConvMeta.GetObject(301) error: %v", err)
		}
		if conv.Title != "General" || conv.LastMessageID != 1001 {
			t.Errorf("seeded conversation 301 = %+v, want Title=\"General\" LastMessageID=1001", conv)
		}

		var mem lite_models.MemberLiteRequest
		if err := redis.ConvMembers.GetObject(ctx, "301:44", &mem); err != nil {
			t.Fatalf("redis.ConvMembers.GetObject(\"301:44\") error: %v", err)
		}
		if mem.UnreadCount != 3 || mem.Settings.IsMuted != 1 || mem.Settings.MuteExpireAt != 1700000123000 {
			t.Errorf("seeded member 301:44 = %+v, want UnreadCount=3 IsMuted=1 MuteExpireAt=1700000123000", mem)
		}
	})
}
