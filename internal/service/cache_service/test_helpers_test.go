package cache_service

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

// ============================================================================
// 1. IN-MEMORY REDIS RESP SERVER (TEST DOUBLE FOR L1 CACHE)
// ============================================================================

type fakeRedisServer struct {
	listener  net.Listener
	mu        sync.RWMutex
	strings   map[string]string
	hashes    map[string]map[string]string
	sets      map[string]map[string]struct{}
	zsets     map[string]map[string]float64
	lists     map[string][]string
	ttls      map[string]time.Duration
	failCmds  map[string]error
	failKeys  map[string]error
	pubsubMu  sync.Mutex
	pubsubSub map[string][]net.Conn
}

func newFakeRedisServer(t *testing.T) (*fakeRedisServer, *goredis.Client) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake redis listener: %v", err)
	}

	srv := &fakeRedisServer{
		listener:  ln,
		strings:   make(map[string]string),
		hashes:    make(map[string]map[string]string),
		sets:      make(map[string]map[string]struct{}),
		zsets:     make(map[string]map[string]float64),
		lists:     make(map[string][]string),
		ttls:      make(map[string]time.Duration),
		failCmds:  make(map[string]error),
		failKeys:  make(map[string]error),
		pubsubSub: make(map[string][]net.Conn),
	}

	go srv.acceptLoop()

	client := goredis.NewClient(&goredis.Options{
		Addr:         ln.Addr().String(),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     10,
	})

	t.Cleanup(func() {
		_ = client.Close()
		_ = ln.Close()
		srv.pubsubMu.Lock()
		for _, conns := range srv.pubsubSub {
			for _, c := range conns {
				_ = c.Close()
			}
		}
		srv.pubsubMu.Unlock()
	})

	return srv, client
}

func setupTestRedis(t *testing.T) *fakeRedisServer {
	t.Helper()
	srv, client := newFakeRedisServer(t)
	infraRedis.Rdb = client
	repoRedis.InitCacheDatabase()
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

func (s *fakeRedisServer) setFailKey(key string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.failKeys, key)
	} else {
		s.failKeys[key] = err
	}
}

func (s *fakeRedisServer) getTTL(key string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttls[key]
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
	reader := bufio.NewReader(conn)
	for {
		args, err := readRESPCommand(reader)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		if strings.ToUpper(args[0]) == "SUBSCRIBE" && len(args) >= 2 {
			ch := args[1]
			s.pubsubMu.Lock()
			s.pubsubSub[ch] = append(s.pubsubSub[ch], conn)
			s.pubsubMu.Unlock()
			// Write subscribe confirmation: *3\r\n$9\r\nsubscribe\r\n$<len>\r\n<ch>\r\n:1\r\n
			resp := fmt.Sprintf("*3\r\n$9\r\nsubscribe\r\n$%d\r\n%s\r\n:1\r\n", len(ch), ch)
			_, _ = conn.Write([]byte(resp))
			continue
		}
		resp := s.execCommand(args)
		_, _ = conn.Write([]byte(resp))
	}
}

func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if len(line) == 0 {
		return nil, nil
	}
	if line[0] != '*' {
		return strings.Fields(line), nil
	}
	count, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		szLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		szLine = strings.TrimSuffix(strings.TrimSuffix(szLine, "\n"), "\r")
		if len(szLine) == 0 || szLine[0] != '$' {
			return nil, fmt.Errorf("expected bulk string, got %q", szLine)
		}
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

func respSimple(str string) string { return "+" + str + "\r\n" }
func respErr(msg string) string    { return "-ERR " + msg + "\r\n" }
func respInt(v int64) string       { return fmt.Sprintf(":%d\r\n", v) }
func respNil() string              { return "$-1\r\n" }
func respBulk(s string) string     { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }
func respArray(items []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%d\r\n", len(items)))
	for _, it := range items {
		sb.WriteString(it)
	}
	return sb.String()
}

func (s *fakeRedisServer) execCommand(args []string) string {
	cmd := strings.ToUpper(args[0])

	s.mu.Lock()
	defer s.mu.Unlock()

	if err, exists := s.failCmds[cmd]; exists {
		return respErr(err.Error())
	}
	if len(args) > 1 {
		if err, exists := s.failKeys[args[1]]; exists {
			return respErr(err.Error())
		}
		if cmd == "EVAL" && len(args) > 3 {
			if err, exists := s.failKeys[args[3]]; exists {
				return respErr(err.Error())
			}
		}
	}

	switch cmd {
	case "PING":
		return respSimple("PONG")

	case "FLUSHDB":
		s.strings = make(map[string]string)
		s.hashes = make(map[string]map[string]string)
		s.sets = make(map[string]map[string]struct{})
		s.zsets = make(map[string]map[string]float64)
		s.lists = make(map[string][]string)
		s.ttls = make(map[string]time.Duration)
		return respSimple("OK")

	case "SET":
		if len(args) < 3 {
			return respErr("wrong number of arguments for SET")
		}
		key, val := args[1], args[2]
		var nx bool
		var exSec, pxMs int64
		for i := 3; i < len(args); i++ {
			switch strings.ToUpper(args[i]) {
			case "NX":
				nx = true
			case "EX":
				if i+1 < len(args) {
					exSec, _ = strconv.ParseInt(args[i+1], 10, 64)
					i++
				}
			case "PX":
				if i+1 < len(args) {
					pxMs, _ = strconv.ParseInt(args[i+1], 10, 64)
					i++
				}
			}
		}
		if nx {
			if _, exists := s.strings[key]; exists {
				return respNil()
			}
		}
		s.strings[key] = val
		if exSec > 0 {
			s.ttls[key] = time.Duration(exSec) * time.Second
		} else if pxMs > 0 {
			s.ttls[key] = time.Duration(pxMs) * time.Millisecond
		}
		return respSimple("OK")

	case "SETNX":
		if len(args) < 3 {
			return respErr("wrong number of arguments for SETNX")
		}
		key, val := args[1], args[2]
		if _, exists := s.strings[key]; exists {
			return respInt(0)
		}
		s.strings[key] = val
		return respInt(1)

	case "GET":
		if len(args) < 2 {
			return respErr("wrong number of arguments for GET")
		}
		val, ok := s.strings[args[1]]
		if !ok {
			return respNil()
		}
		return respBulk(val)

	case "MGET":
		if len(args) < 2 {
			return respErr("wrong number of arguments for MGET")
		}
		out := make([]string, 0, len(args)-1)
		for _, k := range args[1:] {
			if err, fail := s.failKeys[k]; fail {
				return respErr(err.Error())
			}
			if val, ok := s.strings[k]; ok {
				out = append(out, respBulk(val))
			} else {
				out = append(out, respNil())
			}
		}
		return respArray(out)

	case "DEL":
		var deleted int64
		for _, k := range args[1:] {
			var found bool
			if _, ok := s.strings[k]; ok {
				delete(s.strings, k)
				found = true
			}
			if _, ok := s.hashes[k]; ok {
				delete(s.hashes, k)
				found = true
			}
			if _, ok := s.sets[k]; ok {
				delete(s.sets, k)
				found = true
			}
			if _, ok := s.zsets[k]; ok {
				delete(s.zsets, k)
				found = true
			}
			if _, ok := s.lists[k]; ok {
				delete(s.lists, k)
				found = true
			}
			delete(s.ttls, k)
			if found {
				deleted++
			}
		}
		return respInt(deleted)

	case "EXISTS":
		var count int64
		for _, k := range args[1:] {
			_, ok1 := s.strings[k]
			_, ok2 := s.hashes[k]
			_, ok3 := s.sets[k]
			_, ok4 := s.zsets[k]
			_, ok5 := s.lists[k]
			if ok1 || ok2 || ok3 || ok4 || ok5 {
				count++
			}
		}
		return respInt(count)

	case "EXPIRE":
		if len(args) < 3 {
			return respErr("wrong number of arguments for EXPIRE")
		}
		key := args[1]
		sec, _ := strconv.ParseInt(args[2], 10, 64)
		s.ttls[key] = time.Duration(sec) * time.Second
		return respInt(1)

	case "PEXPIRE":
		if len(args) < 3 {
			return respErr("wrong number of arguments for PEXPIRE")
		}
		key := args[1]
		ms, _ := strconv.ParseInt(args[2], 10, 64)
		s.ttls[key] = time.Duration(ms) * time.Millisecond
		return respInt(1)

	case "INCR":
		key := args[1]
		cur, _ := strconv.ParseInt(s.strings[key], 10, 64)
		cur++
		s.strings[key] = strconv.FormatInt(cur, 10)
		return respInt(cur)

	case "KEYS":
		pattern := args[1]
		prefix := strings.TrimSuffix(pattern, "*")
		seen := make(map[string]struct{})
		for k := range s.strings {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.zsets {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.hashes {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.sets {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		out := make([]string, 0, len(seen))
		for k := range seen {
			out = append(out, respBulk(k))
		}
		return respArray(out)

	case "HSET":
		if len(args) < 4 {
			return respErr("wrong number of arguments for HSET")
		}
		key := args[1]
		if s.hashes[key] == nil {
			s.hashes[key] = make(map[string]string)
		}
		var added int64
		for i := 2; i+1 < len(args); i += 2 {
			f, v := args[i], args[i+1]
			if _, exists := s.hashes[key][f]; !exists {
				added++
			}
			s.hashes[key][f] = v
		}
		return respInt(added)

	case "HSETNX":
		key, f, v := args[1], args[2], args[3]
		if s.hashes[key] == nil {
			s.hashes[key] = make(map[string]string)
		}
		if _, exists := s.hashes[key][f]; exists {
			return respInt(0)
		}
		s.hashes[key][f] = v
		return respInt(1)

	case "HGET":
		key, f := args[1], args[2]
		if h, ok := s.hashes[key]; ok {
			if v, ok2 := h[f]; ok2 {
				return respBulk(v)
			}
		}
		return respNil()

	case "HGETALL":
		key := args[1]
		h := s.hashes[key]
		out := make([]string, 0, len(h)*2)
		for f, v := range h {
			out = append(out, respBulk(f), respBulk(v))
		}
		return respArray(out)

	case "HDEL":
		key := args[1]
		h := s.hashes[key]
		if h == nil {
			return respInt(0)
		}
		var removed int64
		for _, f := range args[2:] {
			if _, ok := h[f]; ok {
				delete(h, f)
				removed++
			}
		}
		if len(h) == 0 {
			delete(s.hashes, key)
		}
		return respInt(removed)

	case "HINCRBY":
		key, f := args[1], args[2]
		delta, _ := strconv.ParseInt(args[3], 10, 64)
		if s.hashes[key] == nil {
			s.hashes[key] = make(map[string]string)
		}
		cur, _ := strconv.ParseInt(s.hashes[key][f], 10, 64)
		cur += delta
		s.hashes[key][f] = strconv.FormatInt(cur, 10)
		return respInt(cur)

	case "SADD":
		key := args[1]
		if s.sets[key] == nil {
			s.sets[key] = make(map[string]struct{})
		}
		var added int64
		for _, m := range args[2:] {
			if _, exists := s.sets[key][m]; !exists {
				s.sets[key][m] = struct{}{}
				added++
			}
		}
		return respInt(added)

	case "SREM":
		key := args[1]
		st := s.sets[key]
		if st == nil {
			return respInt(0)
		}
		var removed int64
		for _, m := range args[2:] {
			if _, exists := st[m]; exists {
				delete(st, m)
				removed++
			}
		}
		if len(st) == 0 {
			delete(s.sets, key)
		}
		return respInt(removed)

	case "SMEMBERS":
		key := args[1]
		st := s.sets[key]
		members := make([]string, 0, len(st))
		for m := range st {
			members = append(members, m)
		}
		sort.Strings(members)
		out := make([]string, 0, len(members))
		for _, m := range members {
			out = append(out, respBulk(m))
		}
		return respArray(out)

	case "SCARD":
		key := args[1]
		return respInt(int64(len(s.sets[key])))

	case "RPUSH":
		key := args[1]
		s.lists[key] = append(s.lists[key], args[2:]...)
		return respInt(int64(len(s.lists[key])))

	case "LPUSH":
		key := args[1]
		for _, v := range args[2:] {
			s.lists[key] = append([]string{v}, s.lists[key]...)
		}
		return respInt(int64(len(s.lists[key])))

	case "ZADD":
		key := args[1]
		if s.zsets[key] == nil {
			s.zsets[key] = make(map[string]float64)
		}
		var added int64
		for i := 2; i+1 < len(args); i += 2 {
			score, _ := strconv.ParseFloat(args[i], 64)
			member := args[i+1]
			if _, exists := s.zsets[key][member]; !exists {
				added++
			}
			s.zsets[key][member] = score
		}
		return respInt(added)

	case "ZINCRBY":
		key := args[1]
		inc, _ := strconv.ParseFloat(args[2], 64)
		member := args[3]
		if s.zsets[key] == nil {
			s.zsets[key] = make(map[string]float64)
		}
		s.zsets[key][member] += inc
		return respBulk(strconv.FormatFloat(s.zsets[key][member], 'f', -1, 64))

	case "ZREM":
		key := args[1]
		zs := s.zsets[key]
		if zs == nil {
			return respInt(0)
		}
		var removed int64
		for _, m := range args[2:] {
			if _, exists := zs[m]; exists {
				delete(zs, m)
				removed++
			}
		}
		if len(zs) == 0 {
			delete(s.zsets, key)
		}
		return respInt(removed)

	case "ZCARD":
		key := args[1]
		return respInt(int64(len(s.zsets[key])))

	case "ZSCORE":
		key, member := args[1], args[2]
		if zs, ok := s.zsets[key]; ok {
			if sc, ok2 := zs[member]; ok2 {
				return respBulk(strconv.FormatFloat(sc, 'f', -1, 64))
			}
		}
		return respNil()

	case "ZRANGE", "ZREVRANGE":
		key := args[1]
		start, _ := strconv.ParseInt(args[2], 10, 64)
		stop, _ := strconv.ParseInt(args[3], 10, 64)
		items := s.sortedZSet(key, cmd == "ZREVRANGE")
		sliced := sliceBounds(items, start, stop)
		out := make([]string, 0, len(sliced))
		for _, it := range sliced {
			out = append(out, respBulk(it.member))
		}
		return respArray(out)

	case "ZRANGEBYSCORE", "ZREVRANGEBYSCORE":
		key := args[1]
		var minStr, maxStr string
		rev := cmd == "ZREVRANGEBYSCORE"
		if rev {
			maxStr, minStr = args[2], args[3]
		} else {
			minStr, maxStr = args[2], args[3]
		}
		var offset, count int64 = 0, -1
		for i := 4; i < len(args); i++ {
			if strings.ToUpper(args[i]) == "LIMIT" && i+2 < len(args) {
				offset, _ = strconv.ParseInt(args[i+1], 10, 64)
				count, _ = strconv.ParseInt(args[i+2], 10, 64)
				i += 2
			}
		}
		items := s.sortedZSet(key, rev)
		var matched []string
		for _, it := range items {
			if matchScoreBound(it.score, minStr, true) && matchScoreBound(it.score, maxStr, false) {
				matched = append(matched, it.member)
			}
		}
		if offset > 0 {
			if int(offset) >= len(matched) {
				matched = nil
			} else {
				matched = matched[offset:]
			}
		}
		if count >= 0 && int(count) < len(matched) {
			matched = matched[:count]
		}
		out := make([]string, 0, len(matched))
		for _, m := range matched {
			out = append(out, respBulk(m))
		}
		return respArray(out)

	case "ZRANGEBYLEX":
		key := args[1]
		minLex, maxLex := args[2], args[3]
		var offset, count int64 = 0, -1
		for i := 4; i < len(args); i++ {
			if strings.ToUpper(args[i]) == "LIMIT" && i+2 < len(args) {
				offset, _ = strconv.ParseInt(args[i+1], 10, 64)
				count, _ = strconv.ParseInt(args[i+2], 10, 64)
				i += 2
			}
		}
		items := s.sortedZSet(key, false)
		var matched []string
		for _, it := range items {
			if matchLexBound(it.member, minLex, true) && matchLexBound(it.member, maxLex, false) {
				matched = append(matched, it.member)
			}
		}
		if offset > 0 {
			if int(offset) >= len(matched) {
				matched = nil
			} else {
				matched = matched[offset:]
			}
		}
		if count >= 0 && int(count) < len(matched) {
			matched = matched[:count]
		}
		out := make([]string, 0, len(matched))
		for _, m := range matched {
			out = append(out, respBulk(m))
		}
		return respArray(out)

	case "PUBLISH":
		ch, payload := args[1], args[2]
		s.pubsubMu.Lock()
		subs := append([]net.Conn(nil), s.pubsubSub[ch]...)
		s.pubsubMu.Unlock()
		msg := fmt.Sprintf("*3\r\n$7\r\nmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(ch), ch, len(payload), payload)
		for _, c := range subs {
			_, _ = c.Write([]byte(msg))
		}
		return respInt(int64(len(subs)))

	case "EVAL":
		return s.execEval(args)

	default:
		return respSimple("OK")
	}
}

func (s *fakeRedisServer) execEval(args []string) string {
	if len(args) < 3 {
		return respErr("wrong number of arguments for EVAL")
	}
	script := args[1]
	numKeys, _ := strconv.Atoi(args[2])
	keys := args[3 : 3+numKeys]
	argv := args[3+numKeys:]

	// 1. zaddCapScript
	if strings.Contains(script, "ZREMRANGEBYRANK") {
		key := keys[0]
		score, _ := strconv.ParseFloat(argv[0], 64)
		member := argv[1]
		maxSize, _ := strconv.Atoi(argv[2])
		if s.zsets[key] == nil {
			s.zsets[key] = make(map[string]float64)
		}
		s.zsets[key][member] = score
		if maxSize >= 0 && len(s.zsets[key]) > maxSize {
			asc := s.sortedZSet(key, false)
			toRemove := len(asc) - maxSize
			for i := 0; i < toRemove; i++ {
				delete(s.zsets[key], asc[i].member)
			}
		}
		if sc, ok := s.zsets[key][member]; ok {
			return respBulk(strconv.FormatFloat(sc, 'f', -1, 64))
		}
		return respNil()
	}

	// 2. applyVariablesPatchScript
	if strings.Contains(script, "INCR") && strings.Contains(script, "HDEL") {
		overridesKey, versionKey := keys[0], keys[1]
		if s.hashes[overridesKey] == nil {
			s.hashes[overridesKey] = make(map[string]string)
		}
		setCount, _ := strconv.Atoi(argv[0])
		idx := 1
		for i := 0; i < setCount; i++ {
			k, v := argv[idx], argv[idx+1]
			s.hashes[overridesKey][k] = v
			idx += 2
		}
		delCount, _ := strconv.Atoi(argv[idx])
		idx++
		for i := 0; i < delCount; i++ {
			k := argv[idx]
			delete(s.hashes[overridesKey], k)
			idx++
		}
		ver, _ := strconv.ParseInt(s.strings[versionKey], 10, 64)
		ver++
		s.strings[versionKey] = strconv.FormatInt(ver, 10)
		return respInt(ver)
	}

	// 3. loadVariablesStateScript
	if strings.Contains(script, "HGETALL") {
		overridesKey, versionKey := keys[0], keys[1]
		ver, _ := strconv.ParseInt(s.strings[versionKey], 10, 64)
		h := s.hashes[overridesKey]
		rawItems := make([]string, 0, len(h)*2)
		for k, v := range h {
			rawItems = append(rawItems, respBulk(k), respBulk(v))
		}
		return respArray([]string{respInt(ver), respArray(rawItems)})
	}

	return respNil()
}

type zItem struct {
	member string
	score  float64
}

func (s *fakeRedisServer) sortedZSet(key string, desc bool) []zItem {
	zs := s.zsets[key]
	items := make([]zItem, 0, len(zs))
	for m, sc := range zs {
		items = append(items, zItem{member: m, score: sc})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			if desc {
				return items[i].member > items[j].member
			}
			return items[i].member < items[j].member
		}
		if desc {
			return items[i].score > items[j].score
		}
		return items[i].score < items[j].score
	})
	return items
}

func sliceBounds(items []zItem, start, stop int64) []zItem {
	n := int64(len(items))
	if n == 0 {
		return nil
	}
	if start < 0 {
		start = n + start
	}
	if stop < 0 {
		stop = n + stop
	}
	if start < 0 {
		start = 0
	}
	if start >= n || start > stop {
		return nil
	}
	if stop >= n {
		stop = n - 1
	}
	return items[start : stop+1]
}

func matchScoreBound(val float64, bound string, isMin bool) bool {
	if bound == "-inf" {
		return true
	}
	if bound == "+inf" {
		return true
	}
	exclusive := strings.HasPrefix(bound, "(")
	clean := strings.TrimPrefix(bound, "(")
	bVal, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return false
	}
	if isMin {
		if exclusive {
			return val > bVal
		}
		return val >= bVal
	}
	if exclusive {
		return val < bVal
	}
	return val <= bVal
}

func matchLexBound(member, bound string, isMin bool) bool {
	if bound == "-" || bound == "+" {
		return true
	}
	inclusive := strings.HasPrefix(bound, "[")
	clean := strings.TrimPrefix(strings.TrimPrefix(bound, "["), "(")
	if isMin {
		if inclusive {
			return member >= clean
		}
		return member > clean
	}
	if inclusive {
		return member <= clean
	}
	return member < clean
}

// ============================================================================
// 2. IN-MEMORY POSTGRES SQL DRIVER (TEST DOUBLE FOR L3 COLD STORAGE)
// ============================================================================

type fakeSQLResponse struct {
	cols []string
	rows [][]driver.Value
	err  error
}

type fakeSQLHandler func(query string, args []driver.NamedValue) fakeSQLResponse

var (
	registerSQLOnce sync.Once
	sqlHandlersMu   sync.RWMutex
	sqlHandlers     = make(map[string]fakeSQLHandler)
)

type fakeDriver struct{}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	return &fakeSQLConn{dsn: name}, nil
}

type fakeSQLConn struct {
	dsn string
}

func (c *fakeSQLConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeSQLStmt{dsn: c.dsn, query: query}, nil
}
func (c *fakeSQLConn) Close() error              { return nil }
func (c *fakeSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("not supported") }

func (c *fakeSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	sqlHandlersMu.RLock()
	h := sqlHandlers[c.dsn]
	sqlHandlersMu.RUnlock()
	if h == nil {
		return nil, errors.New("no sql handler registered")
	}
	resp := h(query, args)
	if resp.err != nil {
		return nil, resp.err
	}
	return &fakeSQLRows{cols: resp.cols, rows: resp.rows}, nil
}

type fakeSQLStmt struct {
	dsn   string
	query string
}

func (s *fakeSQLStmt) Close() error  { return nil }
func (s *fakeSQLStmt) NumInput() int { return -1 }
func (s *fakeSQLStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return nil, errors.New("exec not supported")
}
func (s *fakeSQLStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, a := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	sqlHandlersMu.RLock()
	h := sqlHandlers[s.dsn]
	sqlHandlersMu.RUnlock()
	if h == nil {
		return nil, errors.New("no sql handler registered")
	}
	resp := h(s.query, named)
	if resp.err != nil {
		return nil, resp.err
	}
	return &fakeSQLRows{cols: resp.cols, rows: resp.rows}, nil
}

type fakeSQLRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *fakeSQLRows) Columns() []string { return r.cols }
func (r *fakeSQLRows) Close() error      { return nil }
func (r *fakeSQLRows) Next(dest []driver.Value) error {
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
	registerSQLOnce.Do(func() {
		sql.Register("fake_pg_cache_service", &fakeDriver{})
	})
	dsn := fmt.Sprintf("dsn_%s_%d", t.Name(), time.Now().UnixNano())
	sqlHandlersMu.Lock()
	sqlHandlers[dsn] = handler
	sqlHandlersMu.Unlock()

	db, err := sql.Open("fake_pg_cache_service", dsn)
	if err != nil {
		t.Fatalf("failed to open fake sql db: %v", err)
	}
	infraPg.PostgresDB = db

	t.Cleanup(func() {
		_ = db.Close()
		sqlHandlersMu.Lock()
		delete(sqlHandlers, dsn)
		sqlHandlersMu.Unlock()
	})
}

// ============================================================================
// 3. IN-MEMORY MONGODB WIRE PROTOCOL SERVER (TEST DOUBLE FOR L2 STORAGE)
// ============================================================================

type fakeMongoServer struct {
	listener net.Listener
	mu       sync.RWMutex
	docs     map[string][]bson.M
	failColl map[string]bool
	upserted map[string][]bson.M
}

func setupTestMongo(t *testing.T) *fakeMongoServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake mongo listener: %v", err)
	}

	srv := &fakeMongoServer{
		listener: ln,
		docs:     make(map[string][]bson.M),
		failColl: make(map[string]bool),
		upserted: make(map[string][]bson.M),
	}

	go srv.acceptLoop()

	uri := fmt.Sprintf("mongodb://%s/?directConnection=true&serverSelectionTimeoutMS=2000", ln.Addr().String())
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect fake mongo client: %v", err)
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
				st, rem, ok = wiremessage.ReadMsgSectionType(rem)
				if !ok {
					break
				}
				if st == wiremessage.SingleDocument {
					var docBytes []byte
					docBytes, rem, ok = wiremessage.ReadMsgSectionSingleDocument(rem)
					if ok && len(cmdDoc) == 0 {
						cmdDoc = bson.Raw(docBytes)
					}
				} else if st == wiremessage.DocumentSequence {
					_, seqDocs, nextRem, seqOk := wiremessage.ReadMsgSectionDocumentSequence(rem)
					rem = nextRem
					ok = seqOk
					if ok && len(seqDocs) > 0 {
						docsBytes := make([][]byte, 0, len(seqDocs))
						for _, doc := range seqDocs {
							docsBytes = append(docsBytes, []byte(doc))
						}
						// Store upserted updates if present
						m.recordUpdateDocs(cmdDoc, docsBytes)
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

func (m *fakeMongoServer) recordUpdateDocs(cmdDoc bson.Raw, docsBytes [][]byte) {
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

func (m *fakeMongoServer) buildReply(cmdDoc bson.Raw) bson.D {
	elems, err := cmdDoc.Elements()
	if err != nil || len(elems) == 0 {
		return bson.D{{Key: "ok", Value: 1.0}}
	}
	firstKey := elems[0].Key()

	switch strings.ToLower(firstKey) {
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
