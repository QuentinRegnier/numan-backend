package object_cache_service

import (
	"context"
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

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
	"go.mongodb.org/mongo-driver/x/mongo/driver/wiremessage"
)

// verifiedObjMongoServer implements a MongoDB wire-protocol test double using
// the exact go.mongodb.org/mongo-driver v1.17.4 wiremessage and bsoncore APIs
// without any deprecated OpQuery or ReadQuery* calls.
type verifiedObjMongoServer struct {
	listener net.Listener
	mu       sync.RWMutex
	docs     map[string][]bson.M
	failColl map[string]bool
	upserted map[string][]bson.M
}

func setupVerifiedObjMongo(t *testing.T) *verifiedObjMongoServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local tcp port: %v", err)
	}
	srv := &verifiedObjMongoServer{
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
		t.Fatalf("failed to connect mongo client: %v", err)
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

func (m *verifiedObjMongoServer) setDocs(coll string, docs ...bson.M) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[coll] = docs
}

func (m *verifiedObjMongoServer) setFailCollection(coll string, fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failColl[coll] = fail
}

func (m *verifiedObjMongoServer) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		go m.handleConn(conn)
	}
}

func (m *verifiedObjMongoServer) handleConn(conn net.Conn) {
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
			// Skip 4-byte flags and null-terminated collection name C-string
			pos := 4
			for pos < len(body) && body[pos] != 0 {
				pos++
			}
			pos++ // skip null terminator
			if pos+8 > len(body) {
				return
			}
			pos += 8 // skip numberToSkip (int32) and numberToReturn (int32)
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

func (m *verifiedObjMongoServer) recordUpdateDocs(cmdDoc bson.Raw, docsBytes [][]byte) {
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

func (m *verifiedObjMongoServer) buildReply(cmdDoc bson.Raw) bson.D {
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
	default:
		return bson.D{{Key: "ok", Value: 1.0}}
	}
}

func TestMediaObjectCache_CompletePayloadFidelityAndErrors(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("SetMediaInObjectCache and GetMediaFromObjectCache preserve all MediaPayload fields", func(t *testing.T) {
		want := media_models.MediaPayload{
			ID:          101,
			OwnerID:     10,
			StoragePath: "s3://numan-media/users/10/avatar_101.webp",
			Visibility:  true,
			CreatedAt:   1700000001000,
			UpdatedAt:   1700000002000,
		}

		if err := SetMediaInObjectCache(ctx, want); err != nil {
			t.Fatalf("SetMediaInObjectCache() unexpected error: %v", err)
		}

		got, err := GetMediaFromObjectCache(ctx, want.ID)
		if err != nil {
			t.Fatalf("GetMediaFromObjectCache(%d) unexpected error: %v", want.ID, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetMediaFromObjectCache(%d) = %+v, want %+v", want.ID, got, want)
		}
	})

	t.Run("GetMediaFromObjectCache rejects soft-deleted media with 404 AppError", func(t *testing.T) {
		softDeleted := media_models.MediaPayload{
			ID:          102,
			OwnerID:     10,
			StoragePath: "s3://numan-media/users/10/deleted_102.webp",
			Visibility:  false,
			CreatedAt:   1700000003000,
			UpdatedAt:   1700000004000,
		}
		if err := SetMediaInObjectCache(ctx, softDeleted); err != nil {
			t.Fatalf("SetMediaInObjectCache() unexpected error: %v", err)
		}

		_, err := GetMediaFromObjectCache(ctx, softDeleted.ID)
		if err == nil {
			t.Fatal("GetMediaFromObjectCache() for soft-deleted media returned nil error, want 404 NotFound")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusNotFound || appErr.Code != numan_error.CodeNotFound {
			t.Errorf("GetMediaFromObjectCache() error = %+v, want 404 %s AppError", err, numan_error.CodeNotFound)
		}
	})

	t.Run("DeleteMediaFromObjectCache evicts entry and propagates SET/DEL errors as 500 AppError", func(t *testing.T) {
		if err := DeleteMediaFromObjectCache(ctx, 101); err != nil {
			t.Fatalf("DeleteMediaFromObjectCache(101) unexpected error: %v", err)
		}
		if _, err := GetMediaFromObjectCache(ctx, 101); err == nil {
			t.Error("expected cache miss error after DeleteMediaFromObjectCache(101), got nil")
		}

		srv.setFailCmd("SET", errors.New("redis set failure"))
		errSet := SetMediaInObjectCache(ctx, media_models.MediaPayload{
			ID:          103,
			OwnerID:     10,
			StoragePath: "s3://numan-media/users/10/fail.webp",
			Visibility:  true,
		})
		srv.setFailCmd("SET", nil)

		var appErr *numan_error.AppError
		if !errors.As(errSet, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SetMediaInObjectCache() on SET failure = %v, want 500 AppError", errSet)
		}

		srv.setFailCmd("DEL", errors.New("redis del failure"))
		errDel := DeleteMediaFromObjectCache(ctx, 101)
		srv.setFailCmd("DEL", nil)

		if !errors.As(errDel, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("DeleteMediaFromObjectCache() on DEL failure = %v, want 500 AppError", errDel)
		}
	})
}

func TestNotificationObjectCache_CompletePayloadAndBSONFidelity(t *testing.T) {
	srv := setupTestRedis(t)
	mongoSrv := setupVerifiedObjMongo(t)
	ctx := context.Background()

	t.Run("GetNotificationsView returns empty non-nil slice on empty input", func(t *testing.T) {
		empty, err := GetNotificationsView(ctx, nil)
		if err != nil {
			t.Fatalf("GetNotificationsView(nil) error = %v, want nil", err)
		}
		if empty == nil || len(empty) != 0 {
			t.Errorf("GetNotificationsView(nil) = %v, want empty non-nil slice", empty)
		}
	})

	t.Run("hydrates L1 MsgPack and L2 MongoDB BSON with full NotificationPayload fidelity and promotes L2 to L1", func(t *testing.T) {
		notifL1 := notification_models.NotificationPayload{
			ID:        1,
			UserID:    100,
			ActorID:   201,
			Type:      "post_like",
			TargetID:  5001,
			IsRead:    false,
			CreatedAt: 1700000010000,
		}
		if err := SetNotificationInObjectCache(ctx, notifL1); err != nil {
			t.Fatalf("SetNotificationInObjectCache(1) unexpected error: %v", err)
		}

		notifL2Expected := notification_models.NotificationPayload{
			ID:        2,
			UserID:    100,
			ActorID:   202,
			Type:      "new_message",
			TargetID:  6002,
			IsRead:    true,
			CreatedAt: 1700000020000,
		}
		mongoSrv.setDocs("activity.notifications", bson.M{
			"id":         notifL2Expected.ID,
			"user_id":    notifL2Expected.UserID,
			"actor_id":   notifL2Expected.ActorID,
			"type":       notifL2Expected.Type,
			"target_id":  notifL2Expected.TargetID,
			"is_read":    notifL2Expected.IsRead,
			"created_at": notifL2Expected.CreatedAt,
		})

		// Request [2, 1, 999] -> should return [2, 1] preserving caller order and all struct fields
		got, err := GetNotificationsView(ctx, []int64{2, 1, 999})
		if err != nil {
			t.Fatalf("GetNotificationsView([2, 1, 999]) unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("GetNotificationsView() len = %d, want 2 (got %+v)", len(got), got)
		}
		if !reflect.DeepEqual(got[0], notifL2Expected) {
			t.Errorf("got[0] (from L2 BSON) = %+v, want %+v", got[0], notifL2Expected)
		}
		if !reflect.DeepEqual(got[1], notifL1) {
			t.Errorf("got[1] (from L1 MsgPack) = %+v, want %+v", got[1], notifL1)
		}

		// Wait for async L2 -> L1 promotion goroutine and verify L1 now serves notification 2 when Mongo fails
		deadline := time.Now().Add(250 * time.Millisecond)
		for time.Now().Before(deadline) {
			if ok, _ := repoRedis.NotificationsObj.Exists(ctx, int64(2)); ok {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

		mongoSrv.setFailCollection("activity.notifications", true)
		defer mongoSrv.setFailCollection("activity.notifications", false)

		resPromoted, err := GetNotificationsView(ctx, []int64{2})
		if err != nil || len(resPromoted) != 1 {
			t.Fatalf("GetNotificationsView([2]) after L1 promotion = (%+v, %v), want 1 item", resPromoted, err)
		}
		if !reflect.DeepEqual(resPromoted[0], notifL2Expected) {
			t.Errorf("promoted notification 2 = %+v, want %+v", resPromoted[0], notifL2Expected)
		}
	})

	t.Run("SetNotificationInObjectCache returns 500 AppError when Redis SET fails", func(t *testing.T) {
		srv.setFailCmd("SET", errors.New("redis set error"))
		defer srv.setFailCmd("SET", nil)

		err := SetNotificationInObjectCache(ctx, notification_models.NotificationPayload{
			ID:        3,
			UserID:    100,
			ActorID:   203,
			Type:      "new_follower",
			TargetID:  100,
			IsRead:    false,
			CreatedAt: 1700000030000,
		})
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SetNotificationInObjectCache() on SET failure = %v, want 500 AppError", err)
		}
	})
}

func TestObjectCacheAllPayloads_RoundTripFidelity(t *testing.T) {
	setupTestRedis(t)
	setupVerifiedObjMongo(t)
	ctx := context.Background()

	t.Run("PostPayload complete 22-field round-trip in L1 Object Cache", func(t *testing.T) {
		wantPost := post_models.PostPayload{
			ID:                8801,
			UserID:            42,
			Content:           "Complete 22-field post payload",
			Hashtags:          []string{"golang", "testing"},
			IndirectHashtags:  []string{"backend"},
			Identifiers:       []int64{100, 200},
			MediaIDs:          []int64{501},
			Visibility:        1,
			PriorityLevel:     2,
			Location:          "Paris, France",
			LikeCount:         15,
			CommentCount:      4,
			ViewCount:         120,
			ReportCount:       0,
			HasMedia:          true,
			Vector:            []float32{0.25, 0.5, 0.75},
			VectorVersion:     1,
			TelemetryDwellSum: 45.5,
			TelemetryDwellSq:  310.25,
			TelemetryClicks:   9,
			CreatedAt:         1700000100000,
			UpdatedAt:         1700000105000,
		}

		if err := SetPostInObjectCache(ctx, wantPost); err != nil {
			t.Fatalf("SetPostInObjectCache() error: %v", err)
		}
		gotPost, err := GetPostFromObjectCache(ctx, wantPost.ID)
		if err != nil {
			t.Fatalf("GetPostFromObjectCache() error: %v", err)
		}
		if !reflect.DeepEqual(gotPost, wantPost) {
			t.Errorf("GetPostFromObjectCache() = %+v, want %+v", gotPost, wantPost)
		}
	})

	t.Run("SavedPayload ZSET lifecycle via AddSavedToZSET, GetSavedPostIDs, and RemoveSavedFromZSET", func(t *testing.T) {
		savedEntry := saved_models.SavedPayload{
			ID:        9901,
			UserID:    42,
			PostID:    8801,
			CreatedAt: 1700000200000,
		}
		if err := AddSavedToZSET(ctx, savedEntry.UserID, savedEntry.PostID, float64(savedEntry.CreatedAt)); err != nil {
			t.Fatalf("AddSavedToZSET() error: %v", err)
		}
		gotIDs, err := GetSavedPostIDs(ctx, savedEntry.UserID, 0, 10)
		if err != nil {
			t.Fatalf("GetSavedPostIDs() error: %v", err)
		}
		if !reflect.DeepEqual(gotIDs, []int64{savedEntry.PostID}) {
			t.Errorf("GetSavedPostIDs() = %v, want [%d]", gotIDs, savedEntry.PostID)
		}
		if err := RemoveSavedFromZSET(ctx, savedEntry.UserID, savedEntry.PostID); err != nil {
			t.Fatalf("RemoveSavedFromZSET() error: %v", err)
		}
		gotAfterRem, err := GetSavedPostIDs(ctx, savedEntry.UserID, 0, 10)
		if err != nil || len(gotAfterRem) != 0 {
			t.Errorf("GetSavedPostIDs() after RemoveSavedFromZSET = (%v, %v), want empty", gotAfterRem, err)
		}
	})
}
