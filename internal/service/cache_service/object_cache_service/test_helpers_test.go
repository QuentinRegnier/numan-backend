package object_cache_service

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	infraMongo "github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	infraPg "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/wiremessage"
)

type fakeRedisServer struct {
	listener net.Listener
	mu       sync.RWMutex
	strings  map[string]string
	zsets    map[string]map[string]float64
	lists    map[string][]string
	ttls     map[string]time.Duration
	failCmds map[string]error
}

func setupTestRedis(t *testing.T) *fakeRedisServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	srv := &fakeRedisServer{
		listener: ln,
		strings:  make(map[string]string),
		zsets:    make(map[string]map[string]float64),
		lists:    make(map[string][]string),
		ttls:     make(map[string]time.Duration),
		failCmds: make(map[string]error),
	}
	go srv.acceptLoop()

	client := goredis.NewClient(&goredis.Options{
		Addr:         ln.Addr().String(),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	infraRedis.Rdb = client
	repoRedis.InitCacheDatabase()

	t.Cleanup(func() {
		_ = client.Close()
		_ = ln.Close()
	})
	return srv
}

func (s *fakeRedisServer) setFailCmd(cmd string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.failCmds, strings.ToUpper(cmd))
	} else {
		s.failCmds[strings.ToUpper(cmd)] = err
	}
}

func (s *fakeRedisServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeRedisServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		args, err := readRESP(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		resp := s.exec(args)
		_, _ = conn.Write([]byte(resp))
	}
}

func readRESP(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if len(line) == 0 || line[0] != '*' {
		return nil, nil
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		szLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		szLine = strings.TrimSuffix(strings.TrimSuffix(szLine, "\n"), "\r")
		sz, err := strconv.Atoi(szLine[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, sz+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:sz]))
	}
	return args, nil
}

func respBulk(s string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }
func respInt(i int64) string   { return fmt.Sprintf(":%d\r\n", i) }
func respErr(msg string) string {
	return fmt.Sprintf("-ERR %s\r\n", msg)
}

func (s *fakeRedisServer) exec(args []string) string {
	cmd := strings.ToUpper(args[0])
	s.mu.Lock()
	defer s.mu.Unlock()

	if fErr, ok := s.failCmds[cmd]; ok && fErr != nil {
		return respErr(fErr.Error())
	}

	switch cmd {
	case "PING":
		return "+PONG\r\n"
	case "SET":
		if len(args) < 3 {
			return respErr("wrong args")
		}
		s.strings[args[1]] = args[2]
		return "+OK\r\n"
	case "GET":
		val, ok := s.strings[args[1]]
		if !ok {
			return "$-1\r\n"
		}
		return respBulk(val)
	case "MGET":
		keys := args[1:]
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("*%d\r\n", len(keys)))
		for _, k := range keys {
			if val, ok := s.strings[k]; ok {
				sb.WriteString(respBulk(val))
			} else {
				sb.WriteString("$-1\r\n")
			}
		}
		return sb.String()
	case "DEL":
		var deleted int64
		for _, k := range args[1:] {
			if _, ok := s.strings[k]; ok {
				delete(s.strings, k)
				deleted++
			}
			if _, ok := s.zsets[k]; ok {
				delete(s.zsets, k)
				deleted++
			}
		}
		return respInt(deleted)
	case "EXISTS":
		var cnt int64
		for _, k := range args[1:] {
			if _, ok := s.strings[k]; ok {
				cnt++
			} else if z, ok := s.zsets[k]; ok && len(z) > 0 {
				cnt++
			}
		}
		return respInt(cnt)
	case "EXPIRE", "PEXPIRE":
		return respInt(1)
	case "LPUSH", "RPUSH":
		k := args[1]
		s.lists[k] = append(s.lists[k], args[2:]...)
		return respInt(int64(len(s.lists[k])))
	case "ZADD":
		k := args[1]
		if s.zsets[k] == nil {
			s.zsets[k] = make(map[string]float64)
		}
		idx := 2
		for idx < len(args) && (strings.EqualFold(args[idx], "NX") || strings.EqualFold(args[idx], "XX") || strings.EqualFold(args[idx], "GT") || strings.EqualFold(args[idx], "LT") || strings.EqualFold(args[idx], "CH")) {
			idx++
		}
		var added int64
		for idx+1 < len(args) {
			sc, _ := strconv.ParseFloat(args[idx], 64)
			mem := args[idx+1]
			if _, exists := s.zsets[k][mem]; !exists {
				added++
			}
			s.zsets[k][mem] = sc
			idx += 2
		}
		return respInt(added)
	case "ZINCRBY":
		k := args[1]
		delta, _ := strconv.ParseFloat(args[2], 64)
		mem := args[3]
		if s.zsets[k] == nil {
			s.zsets[k] = make(map[string]float64)
		}
		s.zsets[k][mem] += delta
		return respBulk(strconv.FormatFloat(s.zsets[k][mem], 'f', -1, 64))
	case "ZREM":
		k := args[1]
		var rem int64
		if z := s.zsets[k]; z != nil {
			for _, m := range args[2:] {
				if _, ok := z[m]; ok {
					delete(z, m)
					rem++
				}
			}
		}
		return respInt(rem)
	case "ZREVRANGE":
		k := args[1]
		start, _ := strconv.Atoi(args[2])
		stop, _ := strconv.Atoi(args[3])
		z := s.zsets[k]
		type pair struct {
			m string
			s float64
		}
		items := make([]pair, 0, len(z))
		for m, sc := range z {
			items = append(items, pair{m: m, s: sc})
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].s == items[j].s {
				return items[i].m > items[j].m
			}
			return items[i].s > items[j].s
		})
		n := len(items)
		if start < 0 {
			start = n + start
		}
		if stop < 0 {
			stop = n + stop
		}
		if start < 0 {
			start = 0
		}
		if start >= n || stop < start {
			return "*0\r\n"
		}
		if stop >= n {
			stop = n - 1
		}
		sliced := items[start : stop+1]
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("*%d\r\n", len(sliced)))
		for _, it := range sliced {
			sb.WriteString(respBulk(it.m))
		}
		return sb.String()
	case "EVAL", "EVALSHA":
		// zaddCapScript: KEYS[1]=key, ARGV[1]=score, ARGV[2]=member, ARGV[3]=cap
		if len(args) >= 6 {
			k := args[3]
			sc, _ := strconv.ParseFloat(args[4], 64)
			mem := args[5]
			capVal, _ := strconv.Atoi(args[6])
			if s.zsets[k] == nil {
				s.zsets[k] = make(map[string]float64)
			}
			s.zsets[k][mem] = sc
			if capVal > 0 && len(s.zsets[k]) > capVal {
				type pair struct {
					m string
					s float64
				}
				items := make([]pair, 0, len(s.zsets[k]))
				for m, score := range s.zsets[k] {
					items = append(items, pair{m: m, s: score})
				}
				sort.Slice(items, func(i, j int) bool { return items[i].s < items[j].s })
				toRemove := len(items) - capVal
				for i := 0; i < toRemove; i++ {
					delete(s.zsets[k], items[i].m)
				}
			}
			return respInt(1)
		}
		return respInt(1)
	default:
		return "+OK\r\n"
	}
}

// ============================================================================
// 2. IN-MEMORY POSTGRESQL SQL DRIVER FOR OBJECT_CACHE_SERVICE
// ============================================================================

type fakeSQLResponse struct {
	cols []string
	rows [][]driver.Value
	err  error
}

type fakeSQLHandler func(query string, args []driver.NamedValue) fakeSQLResponse

var (
	registerObjSQLOnce sync.Once
	objSQLHandlersMu   sync.RWMutex
	objSQLHandlers     = make(map[string]fakeSQLHandler)
)

type fakeObjDriver struct{}

func (d *fakeObjDriver) Open(name string) (driver.Conn, error) {
	return &fakeObjConn{dsn: name}, nil
}

type fakeObjConn struct{ dsn string }

func (c *fakeObjConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeObjStmt{dsn: c.dsn, query: query}, nil
}
func (c *fakeObjConn) Close() error              { return nil }
func (c *fakeObjConn) Begin() (driver.Tx, error) { return nil, errors.New("not supported") }
func (c *fakeObjConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	objSQLHandlersMu.RLock()
	h := objSQLHandlers[c.dsn]
	objSQLHandlersMu.RUnlock()
	if h == nil {
		return nil, errors.New("no sql handler")
	}
	resp := h(query, args)
	if resp.err != nil {
		return nil, resp.err
	}
	return &fakeObjRows{cols: resp.cols, rows: resp.rows}, nil
}

type fakeObjStmt struct {
	dsn   string
	query string
}

func (s *fakeObjStmt) Close() error  { return nil }
func (s *fakeObjStmt) NumInput() int { return -1 }
func (s *fakeObjStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return nil, errors.New("not supported")
}
func (s *fakeObjStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, a := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	objSQLHandlersMu.RLock()
	h := objSQLHandlers[s.dsn]
	objSQLHandlersMu.RUnlock()
	if h == nil {
		return nil, errors.New("no sql handler")
	}
	resp := h(s.query, named)
	if resp.err != nil {
		return nil, resp.err
	}
	return &fakeObjRows{cols: resp.cols, rows: resp.rows}, nil
}

type fakeObjRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *fakeObjRows) Columns() []string { return r.cols }
func (r *fakeObjRows) Close() error      { return nil }
func (r *fakeObjRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i]
		}
	}
	return nil
}

func setupTestPostgres(t *testing.T, handler fakeSQLHandler) {
	t.Helper()
	registerObjSQLOnce.Do(func() {
		sql.Register("fake_pg_object_cache_service", &fakeObjDriver{})
	})
	dsn := fmt.Sprintf("obj_dsn_%s_%d", t.Name(), time.Now().UnixNano())
	objSQLHandlersMu.Lock()
	objSQLHandlers[dsn] = handler
	objSQLHandlersMu.Unlock()

	db, err := sql.Open("fake_pg_object_cache_service", dsn)
	if err != nil {
		t.Fatalf("failed to open fake sql db: %v", err)
	}
	infraPg.PostgresDB = db

	t.Cleanup(func() {
		_ = db.Close()
		objSQLHandlersMu.Lock()
		delete(objSQLHandlers, dsn)
		objSQLHandlersMu.Unlock()
	})
}

// ============================================================================
// 3. IN-MEMORY MONGODB WIRE PROTOCOL SERVER FOR OBJECT_CACHE_SERVICE
// ============================================================================

type fakeMongoServer struct {
	listener net.Listener
	mu       sync.RWMutex
	docs     map[string][]bson.M
	failColl map[string]bool
}

func setupTestMongo(t *testing.T) *fakeMongoServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen mongo: %v", err)
	}
	srv := &fakeMongoServer{
		listener: ln,
		docs:     make(map[string][]bson.M),
		failColl: make(map[string]bool),
	}
	go srv.acceptLoop()

	uri := fmt.Sprintf("mongodb://%s/?directConnection=true&serverSelectionTimeoutMS=2000", ln.Addr().String())
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect fake mongo: %v", err)
	}
	infraMongo.MongoClient = client
	repoMongo.InitCacheDatabase()

	t.Cleanup(func() {
		_ = client.Disconnect(context.Background())
		_ = ln.Close()
	})
	return srv
}

func (m *fakeMongoServer) setDocs(coll string, docs ...bson.M) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[coll] = docs
}

func (m *fakeMongoServer) setFailCollection(coll string, fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failColl[coll] = fail
}

func (m *fakeMongoServer) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		go m.handleConn(conn)
	}
}

func (m *fakeMongoServer) handleConn(conn net.Conn) {
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
		if opCode == legacyOpQueryCode {
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
		} else if opCode == wiremessage.OpMsg {
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
				if st == wiremessage.SingleDocument {
					var docBytes []byte
					docBytes, rem, okSec = wiremessage.ReadMsgSectionSingleDocument(rem)
					if okSec && len(cmdDoc) == 0 {
						cmdDoc = bson.Raw(docBytes)
					}
				} else if st == wiremessage.DocumentSequence {
					_, _, rem, okSec = wiremessage.ReadMsgSectionDocumentSequence(rem)
					if !okSec {
						break
					}
				}
			}
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
			binary.LittleEndian.PutUint32(dst[idx:], uint32(len(dst)))
			_, _ = conn.Write(dst)
		} else {
			var dst []byte
			idx, dst := wiremessage.AppendHeaderStart(dst, 1, reqID, wiremessage.OpMsg)
			dst = wiremessage.AppendMsgFlags(dst, 0)
			dst = wiremessage.AppendMsgSectionType(dst, wiremessage.SingleDocument)
			dst = append(dst, replyBytes...)
			binary.LittleEndian.PutUint32(dst[idx:], uint32(len(dst)))
			_, _ = conn.Write(dst)
		}
	}
}

func (m *fakeMongoServer) buildReply(cmdDoc bson.Raw) bson.D {
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
			return bson.D{{Key: "ok", Value: 0.0}, {Key: "errmsg", Value: "simulated mongo error"}, {Key: "code", Value: int32(1)}}
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
