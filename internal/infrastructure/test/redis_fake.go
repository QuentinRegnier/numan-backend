package test

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/redis/go-redis/v9"
)

type geoPoint struct {
	lon float64
	lat float64
}

type FakeRedisServer struct {
	listener           net.Listener
	mu                 sync.RWMutex
	strings            map[string]string
	hashes             map[string]map[string]string
	sets               map[string]map[string]struct{}
	hlls               map[string]map[string]struct{}
	zsets              map[string]map[string]float64
	geos               map[string]map[string]geoPoint
	lists              map[string][]string
	ttls               map[string]time.Duration
	failCmds           map[string]error
	failKeys           map[string]error
	pubsubMu           sync.Mutex
	pubsubSub          map[string][]net.Conn
	spubsubSub         map[string][]net.Conn
	StrictClusterSlots bool
	SPublishCalls      atomic.Int64
	SSubscribeCalls    atomic.Int64
	InfoMemoryUsed     int64
	InfoMemoryMax      int64
	InfoEvictedKeys    int64
}

func newFakeRedisServer(t *testing.T) (*FakeRedisServer, *goredis.Client) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake redis listener: %v", err)
	}

	srv := &FakeRedisServer{
		listener:        ln,
		strings:         make(map[string]string),
		hashes:          make(map[string]map[string]string),
		sets:            make(map[string]map[string]struct{}),
		hlls:            make(map[string]map[string]struct{}),
		zsets:           make(map[string]map[string]float64),
		geos:            make(map[string]map[string]geoPoint),
		lists:           make(map[string][]string),
		ttls:            make(map[string]time.Duration),
		failCmds:        make(map[string]error),
		failKeys:        make(map[string]error),
		pubsubSub:       make(map[string][]net.Conn),
		spubsubSub:      make(map[string][]net.Conn),
		InfoMemoryUsed:  128 * 1024 * 1024,
		InfoMemoryMax:   1024 * 1024 * 1024,
		InfoEvictedKeys: 0,
	}

	go srv.acceptLoop()

	client := goredis.NewClient(&goredis.Options{
		Addr:            ln.Addr().String(),
		DialTimeout:     2 * time.Second,
		ReadTimeout:     2 * time.Second,
		WriteTimeout:    2 * time.Second,
		PoolSize:        10,
		Protocol:        2,
		DisableIdentity: true,
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
		for _, conns := range srv.spubsubSub {
			for _, c := range conns {
				_ = c.Close()
			}
		}
		srv.pubsubMu.Unlock()
	})

	return srv, client
}

func SetupTestRedis(t *testing.T) *FakeRedisServer {
	t.Helper()
	srv, client := newFakeRedisServer(t)
	infraRedis.Rdb = client
	infraRedis.RdbCore = client
	infraRedis.RdbObjects = client
	infraRedis.RdbPubSub = client
	repoRedis.InitCacheDatabase()
	return srv
}

// SetupTestRedisMultiTier démarre 3 serveurs FakeRedis indépendants (Core, Objects, PubSub) pour tester l'isolation des tiers.
func SetupTestRedisMultiTier(t *testing.T) (coreSrv, objectsSrv, pubsubSrv *FakeRedisServer) {
	t.Helper()
	coreSrv, coreClient := newFakeRedisServer(t)
	objectsSrv, objectsClient := newFakeRedisServer(t)
	pubsubSrv, pubsubClient := newFakeRedisServer(t)
	infraRedis.Rdb = coreClient
	infraRedis.RdbCore = coreClient
	infraRedis.RdbObjects = objectsClient
	infraRedis.RdbPubSub = pubsubClient
	repoRedis.InitCacheDatabase()
	return coreSrv, objectsSrv, pubsubSrv
}

// SetupTestRedisCluster démarre un FakeRedisServer avec vérification stricte CRC16 CROSSSLOT
// connecté via un vrai *redis.ClusterClient go-redis/v9.
func SetupTestRedisCluster(t *testing.T) *FakeRedisServer {
	t.Helper()
	srv, _ := newFakeRedisServer(t)
	srv.StrictClusterSlots = true
	addr := srv.listener.Addr().String()

	clusterClient := goredis.NewClusterClient(&goredis.ClusterOptions{
		Addrs:           []string{addr},
		DialTimeout:     2 * time.Second,
		ReadTimeout:     2 * time.Second,
		WriteTimeout:    2 * time.Second,
		PoolSize:        10,
		Protocol:        2,
		DisableIdentity: true,
	})
	t.Cleanup(func() {
		_ = clusterClient.Close()
	})

	infraRedis.Rdb = clusterClient
	infraRedis.RdbCore = clusterClient
	infraRedis.RdbObjects = clusterClient
	infraRedis.RdbPubSub = clusterClient
	repoRedis.InitCacheDatabase()
	return srv
}

func (s *FakeRedisServer) CloseListener() {
	_ = s.listener.Close()
}

func (s *FakeRedisServer) Addr() string {
	return s.listener.Addr().String()
}

func (s *FakeRedisServer) SetFailCmd(cmd string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.failCmds, strings.ToUpper(cmd))
	} else {
		s.failCmds[strings.ToUpper(cmd)] = err
	}
}

func (s *FakeRedisServer) setFailKey(key string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.failKeys, key)
	} else {
		s.failKeys[key] = err
	}
}

func (s *FakeRedisServer) getTTL(key string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttls[key]
}

func (s *FakeRedisServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *FakeRedisServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	var inMulti bool
	var queued [][]string

	for {
		args, err := readRESPCommand(reader)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		upperCmd := strings.ToUpper(args[0])

		if upperCmd == "MULTI" {
			s.mu.RLock()
			failErr, failed := s.failCmds["MULTI"]
			s.mu.RUnlock()
			if failed {
				_, _ = conn.Write([]byte(respErr(failErr.Error())))
				continue
			}
			inMulti = true
			queued = nil
			_, _ = conn.Write([]byte(respSimple("OK")))
			continue
		}

		if upperCmd == "DISCARD" {
			inMulti = false
			queued = nil
			_, _ = conn.Write([]byte(respSimple("OK")))
			continue
		}

		if upperCmd == "EXEC" {
			if !inMulti {
				_, _ = conn.Write([]byte(respErr("EXEC without MULTI")))
				continue
			}
			inMulti = false
			batch := queued
			queued = nil

			s.mu.Lock()
			if failErr, failed := s.failCmds["EXEC"]; failed {
				s.mu.Unlock()
				_, _ = conn.Write([]byte(respErr(failErr.Error())))
				continue
			}
			results := make([]string, 0, len(batch))
			for _, cmdArgs := range batch {
				results = append(results, s.execCommandLocked(cmdArgs))
			}
			s.mu.Unlock()
			_, _ = conn.Write([]byte(respArray(results)))
			continue
		}

		if inMulti {
			queued = append(queued, args)
			_, _ = conn.Write([]byte(respSimple("QUEUED")))
			continue
		}

		if (upperCmd == "SUBSCRIBE" || upperCmd == "SSUBSCRIBE") && len(args) >= 2 {
			isSharded := upperCmd == "SSUBSCRIBE"
			s.pubsubMu.Lock()
			for _, ch := range args[1:] {
				if isSharded {
					s.spubsubSub[ch] = append(s.spubsubSub[ch], conn)
					s.SSubscribeCalls.Add(1)
				} else {
					s.pubsubSub[ch] = append(s.pubsubSub[ch], conn)
				}
			}
			s.pubsubMu.Unlock()
			kind := "subscribe"
			if isSharded {
				kind = "ssubscribe"
			}
			for i, ch := range args[1:] {
				resp := fmt.Sprintf("*3\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n:%d\r\n", len(kind), kind, len(ch), ch, i+1)
				_, _ = conn.Write([]byte(resp))
			}
			continue
		}
		if upperCmd == "UNSUBSCRIBE" || upperCmd == "SUNSUBSCRIBE" {
			isSharded := upperCmd == "SUNSUBSCRIBE"
			kind := "unsubscribe"
			if isSharded {
				kind = "sunsubscribe"
			}
			chs := args[1:]
			if len(chs) == 0 {
				chs = []string{""}
			}
			s.pubsubMu.Lock()
			for _, ch := range chs {
				targetMap := s.pubsubSub
				if isSharded {
					targetMap = s.spubsubSub
				}
				if list, ok := targetMap[ch]; ok {
					filtered := list[:0]
					for _, c := range list {
						if c != conn {
							filtered = append(filtered, c)
						}
					}
					if len(filtered) == 0 {
						delete(targetMap, ch)
					} else {
						targetMap[ch] = filtered
					}
				}
			}
			s.pubsubMu.Unlock()
			for _, ch := range chs {
				resp := fmt.Sprintf("*3\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n:0\r\n", len(kind), kind, len(ch), ch)
				_, _ = conn.Write([]byte(resp))
			}
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

func (s *FakeRedisServer) execCommand(args []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execCommandLocked(args)
}

func (s *FakeRedisServer) execCommandLocked(args []string) string {
	cmd := strings.ToUpper(args[0])

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

	if s.StrictClusterSlots {
		if slotErr := checkCrossSlot(cmd, args); slotErr != "" {
			return slotErr
		}
	}

	switch cmd {
	case "PING":
		return respSimple("PONG")

	case "HELLO":
		mode := "standalone"
		if s.StrictClusterSlots {
			mode = "cluster"
		}
		return respArray([]string{
			respBulk("server"), respBulk("redis"),
			respBulk("version"), respBulk("7.2.0"),
			respBulk("proto"), respInt(2),
			respBulk("id"), respInt(1),
			respBulk("mode"), respBulk(mode),
			respBulk("role"), respBulk("master"),
		})

	case "CLIENT", "READONLY":
		return respSimple("OK")

	case "CLUSTER":
		if len(args) >= 2 && strings.ToUpper(args[1]) == "SLOTS" {
			host, portStr, _ := net.SplitHostPort(s.listener.Addr().String())
			port, _ := strconv.ParseInt(portStr, 10, 64)
			nodeArr := respArray([]string{respBulk(host), respInt(port), respBulk("fake-node-1")})
			slotEntry := respArray([]string{respInt(0), respInt(16383), nodeArr})
			return respArray([]string{slotEntry})
		}
		if len(args) >= 2 && strings.ToUpper(args[1]) == "NODES" {
			addr := s.listener.Addr().String()
			return respBulk(fmt.Sprintf("fake-node-1 %s@16379 myself,master - 0 0 1 connected 0-16383\n", addr))
		}
		return respSimple("OK")

	case "INFO":
		section := ""
		if len(args) >= 2 {
			section = strings.ToLower(args[1])
		}
		switch section {
		case "memory":
			return respBulk(fmt.Sprintf("# Memory\r\nused_memory:%d\r\nmaxmemory:%d\r\n", s.InfoMemoryUsed, s.InfoMemoryMax))
		case "stats":
			return respBulk(fmt.Sprintf("# Stats\r\nevicted_keys:%d\r\n", s.InfoEvictedKeys))
		case "clients":
			return respBulk("# Clients\r\nconnected_clients:1\r\n")
		default:
			return respBulk(fmt.Sprintf("# Memory\r\nused_memory:%d\r\nmaxmemory:%d\r\n# Stats\r\nevicted_keys:%d\r\n# Clients\r\nconnected_clients:1\r\n",
				s.InfoMemoryUsed, s.InfoMemoryMax, s.InfoEvictedKeys))
		}

	case "PUBSUB":
		if len(args) >= 2 && strings.ToUpper(args[1]) == "CHANNELS" {
			s.pubsubMu.Lock()
			chs := make([]string, 0, len(s.pubsubSub)+len(s.spubsubSub))
			for ch := range s.pubsubSub {
				chs = append(chs, respBulk(ch))
			}
			for ch := range s.spubsubSub {
				chs = append(chs, respBulk(ch))
			}
			s.pubsubMu.Unlock()
			return respArray(chs)
		}
		return respArray(nil)

	case "FLUSHDB":
		s.strings = make(map[string]string)
		s.hashes = make(map[string]map[string]string)
		s.sets = make(map[string]map[string]struct{})
		s.hlls = make(map[string]map[string]struct{})
		s.zsets = make(map[string]map[string]float64)
		s.geos = make(map[string]map[string]geoPoint)
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
			if _, ok := s.hlls[k]; ok {
				delete(s.hlls, k)
				found = true
			}
			if _, ok := s.zsets[k]; ok {
				delete(s.zsets, k)
				found = true
			}
			if _, ok := s.geos[k]; ok {
				delete(s.geos, k)
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
			_, ok6 := s.hlls[k]
			if ok1 || ok2 || ok3 || ok4 || ok5 || ok6 {
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
		if len(args) >= 4 && strings.ToUpper(args[3]) == "NX" {
			if existingTTL, hasTTL := s.ttls[key]; hasTTL && existingTTL > 0 {
				return respInt(0)
			}
		}
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

	case "TTL":
		if len(args) < 2 {
			return respErr("wrong number of arguments for TTL")
		}
		key := args[1]
		_, ok1 := s.strings[key]
		_, ok2 := s.hashes[key]
		_, ok3 := s.sets[key]
		_, ok4 := s.zsets[key]
		_, ok5 := s.lists[key]
		_, ok6 := s.geos[key]
		_, ok7 := s.hlls[key]
		if !ok1 && !ok2 && !ok3 && !ok4 && !ok5 && !ok6 && !ok7 {
			return respInt(-2)
		}
		ttl, hasTTL := s.ttls[key]
		if !hasTTL || ttl <= 0 {
			return respInt(-1)
		}
		return respInt(int64(ttl / time.Second))

	case "PTTL":
		if len(args) < 2 {
			return respErr("wrong number of arguments for PTTL")
		}
		key := args[1]
		_, ok1 := s.strings[key]
		_, ok2 := s.hashes[key]
		_, ok3 := s.sets[key]
		_, ok4 := s.zsets[key]
		_, ok5 := s.lists[key]
		_, ok6 := s.geos[key]
		_, ok7 := s.hlls[key]
		if !ok1 && !ok2 && !ok3 && !ok4 && !ok5 && !ok6 && !ok7 {
			return respInt(-2)
		}
		ttl, hasTTL := s.ttls[key]
		if !hasTTL || ttl <= 0 {
			return respInt(-1)
		}
		return respInt(int64(ttl / time.Millisecond))

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
		for k := range s.hlls {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.lists {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.geos {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		out := make([]string, 0, len(seen))
		for k := range seen {
			out = append(out, respBulk(k))
		}
		return respArray(out)

	case "SCAN":
		pattern := "*"
		for i := 2; i+1 < len(args); i++ {
			if strings.ToUpper(args[i]) == "MATCH" {
				pattern = args[i+1]
				i++
			}
		}
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
		for k := range s.hlls {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.lists {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		for k := range s.geos {
			if strings.HasPrefix(k, prefix) {
				seen[k] = struct{}{}
			}
		}
		keysOut := make([]string, 0, len(seen))
		for k := range seen {
			keysOut = append(keysOut, respBulk(k))
		}
		return respArray([]string{respBulk("0"), respArray(keysOut)})

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

	case "HMGET":
		if len(args) < 3 {
			return respErr("wrong number of arguments for HMGET")
		}
		key := args[1]
		h := s.hashes[key]
		out := make([]string, 0, len(args)-2)
		for _, f := range args[2:] {
			if h != nil {
				if v, ok := h[f]; ok {
					out = append(out, respBulk(v))
					continue
				}
			}
			out = append(out, respNil())
		}
		return respArray(out)

	case "HEXISTS":
		if len(args) < 3 {
			return respErr("wrong number of arguments for HEXISTS")
		}
		key, f := args[1], args[2]
		if h, ok := s.hashes[key]; ok {
			if _, ok2 := h[f]; ok2 {
				return respInt(1)
			}
		}
		return respInt(0)

	case "HLEN":
		if len(args) < 2 {
			return respErr("wrong number of arguments for HLEN")
		}
		key := args[1]
		return respInt(int64(len(s.hashes[key])))

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

	case "PFADD":
		if len(args) < 2 {
			return respErr("wrong number of arguments for PFADD")
		}
		key := args[1]
		if s.hlls[key] == nil {
			s.hlls[key] = make(map[string]struct{})
		}
		var modified int64
		for _, el := range args[2:] {
			if _, exists := s.hlls[key][el]; !exists {
				s.hlls[key][el] = struct{}{}
				modified = 1
			}
		}
		return respInt(modified)

	case "PFCOUNT":
		if len(args) < 2 {
			return respErr("wrong number of arguments for PFCOUNT")
		}
		union := make(map[string]struct{})
		for _, k := range args[1:] {
			if err, fail := s.failKeys[k]; fail {
				return respErr(err.Error())
			}
			for el := range s.hlls[k] {
				union[el] = struct{}{}
			}
		}
		return respInt(int64(len(union)))

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

	case "SISMEMBER":
		key, member := args[1], args[2]
		if st := s.sets[key]; st != nil {
			if _, ok := st[member]; ok {
				return respInt(1)
			}
		}
		return respInt(0)

	case "SINTER":
		if len(args) < 2 {
			return respErr("wrong number of arguments for SINTER")
		}
		firstKey := args[1]
		firstSet := s.sets[firstKey]
		if len(firstSet) == 0 {
			return respArray(nil)
		}
		var matched []string
		for member := range firstSet {
			inAll := true
			for _, otherKey := range args[2:] {
				otherSet := s.sets[otherKey]
				if otherSet == nil {
					inAll = false
					break
				}
				if _, ok := otherSet[member]; !ok {
					inAll = false
					break
				}
			}
			if inAll {
				matched = append(matched, member)
			}
		}
		sort.Strings(matched)
		out := make([]string, 0, len(matched))
		for _, m := range matched {
			out = append(out, respBulk(m))
		}
		return respArray(out)

	case "CF.RESERVE":
		if len(args) < 3 {
			return respErr("wrong number of arguments for CF.RESERVE")
		}
		key := args[1]
		if s.sets[key] == nil {
			s.sets[key] = make(map[string]struct{})
		}
		return respSimple("OK")

	case "CF.ADD", "CF.ADDNX":
		if len(args) < 3 {
			return respErr("wrong number of arguments for CF.ADD")
		}
		key, item := args[1], args[2]
		if s.sets[key] == nil {
			s.sets[key] = make(map[string]struct{})
		}
		if cmd == "CF.ADDNX" {
			if _, exists := s.sets[key][item]; exists {
				return respInt(0)
			}
		}
		s.sets[key][item] = struct{}{}
		return respInt(1)

	case "CF.EXISTS":
		if len(args) < 3 {
			return respErr("wrong number of arguments for CF.EXISTS")
		}
		key, item := args[1], args[2]
		if st := s.sets[key]; st != nil {
			if _, exists := st[item]; exists {
				return respInt(1)
			}
		}
		return respInt(0)

	case "CF.MEXISTS":
		if len(args) < 3 {
			return respErr("wrong number of arguments for CF.MEXISTS")
		}
		key := args[1]
		st := s.sets[key]
		out := make([]string, 0, len(args)-2)
		for _, item := range args[2:] {
			if st != nil {
				if _, exists := st[item]; exists {
					out = append(out, respInt(1))
					continue
				}
			}
			out = append(out, respInt(0))
		}
		return respArray(out)

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

	case "LPOP":
		key := args[1]
		lst := s.lists[key]
		if len(lst) == 0 {
			return respNil()
		}
		if len(args) >= 3 {
			count, err := strconv.Atoi(args[2])
			if err == nil && count > 0 {
				if count > len(lst) {
					count = len(lst)
				}
				popped := lst[:count]
				if count == len(lst) {
					delete(s.lists, key)
				} else {
					s.lists[key] = lst[count:]
				}
				out := make([]string, 0, len(popped))
				for _, v := range popped {
					out = append(out, respBulk(v))
				}
				return respArray(out)
			}
		}
		val := lst[0]
		if len(lst) == 1 {
			delete(s.lists, key)
		} else {
			s.lists[key] = lst[1:]
		}
		return respBulk(val)

	case "BLPOP":
		if len(args) < 3 {
			return respErr("wrong number of arguments for BLPOP")
		}
		for _, key := range args[1 : len(args)-1] {
			lst := s.lists[key]
			if len(lst) > 0 {
				val := lst[0]
				if len(lst) == 1 {
					delete(s.lists, key)
				} else {
					s.lists[key] = lst[1:]
				}
				return respArray([]string{respBulk(key), respBulk(val)})
			}
		}
		return respNil()

	case "LINDEX":
		if len(args) < 3 {
			return respErr("wrong number of arguments for LINDEX")
		}
		key := args[1]
		idx, _ := strconv.Atoi(args[2])
		lst := s.lists[key]
		if idx < 0 {
			idx = len(lst) + idx
		}
		if idx < 0 || idx >= len(lst) {
			return respNil()
		}
		return respBulk(lst[idx])

	case "RPOP":
		key := args[1]
		lst := s.lists[key]
		if len(lst) == 0 {
			return respNil()
		}
		val := lst[len(lst)-1]
		if len(lst) == 1 {
			delete(s.lists, key)
		} else {
			s.lists[key] = lst[:len(lst)-1]
		}
		return respBulk(val)

	case "LLEN":
		key := args[1]
		return respInt(int64(len(s.lists[key])))

	case "LRANGE":
		if len(args) < 4 {
			return respErr("wrong number of arguments for LRANGE")
		}
		key := args[1]
		start, _ := strconv.ParseInt(args[2], 10, 64)
		stop, _ := strconv.ParseInt(args[3], 10, 64)
		lst := s.lists[key]
		n := int64(len(lst))
		if n == 0 {
			return respArray(nil)
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
			return respArray(nil)
		}
		if stop >= n {
			stop = n - 1
		}
		slice := lst[start : stop+1]
		out := make([]string, 0, len(slice))
		for _, item := range slice {
			out = append(out, respBulk(item))
		}
		return respArray(out)

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
		gs := s.geos[key]
		if zs == nil && gs == nil {
			return respInt(0)
		}
		var removed int64
		for _, m := range args[2:] {
			var found bool
			if zs != nil {
				if _, exists := zs[m]; exists {
					delete(zs, m)
					found = true
				}
			}
			if gs != nil {
				if _, exists := gs[m]; exists {
					delete(gs, m)
					found = true
				}
			}
			if found {
				removed++
			}
		}
		if len(zs) == 0 {
			delete(s.zsets, key)
		}
		if len(gs) == 0 {
			delete(s.geos, key)
		}
		return respInt(removed)

	case "ZREMRANGEBYRANK":
		if len(args) < 4 {
			return respErr("wrong number of arguments for ZREMRANGEBYRANK")
		}
		key := args[1]
		start, _ := strconv.ParseInt(args[2], 10, 64)
		stop, _ := strconv.ParseInt(args[3], 10, 64)
		items := s.sortedZSet(key, false)
		toRemove := sliceBounds(items, start, stop)
		var removed int64
		for _, it := range toRemove {
			delete(s.zsets[key], it.member)
			removed++
		}
		if len(s.zsets[key]) == 0 {
			delete(s.zsets, key)
		}
		return respInt(removed)

	case "ZREMRANGEBYSCORE":
		if len(args) < 4 {
			return respErr("wrong number of arguments for ZREMRANGEBYSCORE")
		}
		key := args[1]
		minStr, maxStr := args[2], args[3]
		zs := s.zsets[key]
		var removed int64
		for m, sc := range zs {
			if matchScoreBound(sc, minStr, true) && matchScoreBound(sc, maxStr, false) {
				delete(zs, m)
				removed++
			}
		}
		if len(zs) == 0 {
			delete(s.zsets, key)
		}
		return respInt(removed)

	case "ZUNIONSTORE":
		if len(args) < 4 {
			return respErr("wrong number of arguments for ZUNIONSTORE")
		}
		destKey := args[1]
		numKeys, _ := strconv.Atoi(args[2])
		if len(args) < 3+numKeys {
			return respErr("syntax error in ZUNIONSTORE")
		}
		srcKeys := args[3 : 3+numKeys]
		aggregate := "SUM"
		for i := 3 + numKeys; i+1 < len(args); i++ {
			if strings.ToUpper(args[i]) == "AGGREGATE" {
				aggregate = strings.ToUpper(args[i+1])
				i++
			}
		}
		merged := make(map[string]float64)
		for _, sk := range srcKeys {
			for m, sc := range s.zsets[sk] {
				prev, exists := merged[m]
				if !exists {
					merged[m] = sc
				} else {
					switch aggregate {
					case "MAX":
						if sc > prev {
							merged[m] = sc
						}
					case "MIN":
						if sc < prev {
							merged[m] = sc
						}
					default:
						merged[m] = prev + sc
					}
				}
			}
		}
		if len(merged) > 0 {
			s.zsets[destKey] = merged
		} else {
			delete(s.zsets, destKey)
		}
		return respInt(int64(len(merged)))

	case "GEOADD":
		if len(args) < 5 {
			return respErr("wrong number of arguments for GEOADD")
		}
		key := args[1]
		if s.geos[key] == nil {
			s.geos[key] = make(map[string]geoPoint)
		}
		if s.zsets[key] == nil {
			s.zsets[key] = make(map[string]float64)
		}
		// Skip optional flags NX, XX, CH
		idx := 2
		chFlag := false
		for idx < len(args) {
			up := strings.ToUpper(args[idx])
			if up == "NX" || up == "XX" {
				idx++
			} else if up == "CH" {
				chFlag = true
				idx++
			} else {
				break
			}
		}
		var added int64
		for i := idx; i+2 < len(args); i += 3 {
			lon, err1 := strconv.ParseFloat(args[i], 64)
			lat, err2 := strconv.ParseFloat(args[i+1], 64)
			if err1 != nil || err2 != nil {
				return respErr("invalid longitude or latitude")
			}
			member := args[i+2]
			prev, exists := s.geos[key][member]
			if !exists {
				added++
			} else if chFlag && (prev.lon != lon || prev.lat != lat) {
				added++
			}
			s.geos[key][member] = geoPoint{lon: lon, lat: lat}
			s.zsets[key][member] = 0
		}
		return respInt(added)

	case "GEOPOS":
		if len(args) < 2 {
			return respErr("wrong number of arguments for GEOPOS")
		}
		key := args[1]
		gs := s.geos[key]
		out := make([]string, 0, len(args)-2)
		for _, m := range args[2:] {
			if gs != nil {
				if pt, ok := gs[m]; ok {
					out = append(out, respArray([]string{
						respBulk(strconv.FormatFloat(pt.lon, 'f', 6, 64)),
						respBulk(strconv.FormatFloat(pt.lat, 'f', 6, 64)),
					}))
					continue
				}
			}
			out = append(out, respNil())
		}
		return respArray(out)

	case "GEOSEARCH":
		if len(args) < 2 {
			return respErr("wrong number of arguments for GEOSEARCH")
		}
		key := args[1]
		gs := s.geos[key]
		if len(gs) == 0 {
			return respArray(nil)
		}
		var centerLon, centerLat float64
		var byBox, byRadius bool
		var widthKm, heightKm, radiusKm float64
		var count int
		var sortOrder string

		for i := 2; i < len(args); i++ {
			token := strings.ToUpper(args[i])
			switch token {
			case "FROMLONLAT":
				if i+2 < len(args) {
					centerLon, _ = strconv.ParseFloat(args[i+1], 64)
					centerLat, _ = strconv.ParseFloat(args[i+2], 64)
					i += 2
				}
			case "FROMMEMBER":
				if i+1 < len(args) {
					if pt, ok := gs[args[i+1]]; ok {
						centerLon = pt.lon
						centerLat = pt.lat
					}
					i++
				}
			case "BYBOX":
				if i+3 < len(args) {
					byBox = true
					w, _ := strconv.ParseFloat(args[i+1], 64)
					h, _ := strconv.ParseFloat(args[i+2], 64)
					unit := strings.ToLower(args[i+3])
					widthKm = toKilometers(w, unit)
					heightKm = toKilometers(h, unit)
					i += 3
				}
			case "BYRADIUS":
				if i+2 < len(args) {
					byRadius = true
					r, _ := strconv.ParseFloat(args[i+1], 64)
					unit := strings.ToLower(args[i+2])
					radiusKm = toKilometers(r, unit)
					i += 2
				}
			case "ASC", "DESC":
				sortOrder = token
			case "COUNT":
				if i+1 < len(args) {
					count, _ = strconv.Atoi(args[i+1])
					i++
					if i+1 < len(args) && strings.ToUpper(args[i+1]) == "ANY" {
						i++
					}
				}
			}
		}

		type geoCandidate struct {
			member string
			distKm float64
		}
		var matched []geoCandidate
		for member, pt := range gs {
			distKm := haversineKm(centerLat, centerLon, pt.lat, pt.lon)
			if byRadius {
				if distKm <= radiusKm {
					matched = append(matched, geoCandidate{member: member, distKm: distKm})
				}
			} else if byBox {
				// Rectangular bounding box approximation around centerLon, centerLat
				latDiffKm := haversineKm(centerLat, centerLon, pt.lat, centerLon)
				lonDiffKm := haversineKm(pt.lat, centerLon, pt.lat, pt.lon)
				if latDiffKm <= (heightKm/2.0)+0.001 && lonDiffKm <= (widthKm/2.0)+0.001 {
					matched = append(matched, geoCandidate{member: member, distKm: distKm})
				}
			} else {
				matched = append(matched, geoCandidate{member: member, distKm: distKm})
			}
		}

		sort.Slice(matched, func(i, j int) bool {
			if sortOrder == "DESC" {
				if matched[i].distKm == matched[j].distKm {
					return matched[i].member > matched[j].member
				}
				return matched[i].distKm > matched[j].distKm
			}
			if matched[i].distKm == matched[j].distKm {
				return matched[i].member < matched[j].member
			}
			return matched[i].distKm < matched[j].distKm
		})

		if count > 0 && len(matched) > count {
			matched = matched[:count]
		}

		out := make([]string, 0, len(matched))
		for _, c := range matched {
			out = append(out, respBulk(c.member))
		}
		return respArray(out)

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
		withScores := len(args) >= 5 && strings.ToUpper(args[4]) == "WITHSCORES"
		items := s.sortedZSet(key, cmd == "ZREVRANGE")
		sliced := sliceBounds(items, start, stop)
		out := make([]string, 0, len(sliced)*2)
		for _, it := range sliced {
			out = append(out, respBulk(it.member))
			if withScores {
				out = append(out, respBulk(strconv.FormatFloat(it.score, 'f', -1, 64)))
			}
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
		withScores := false
		for i := 4; i < len(args); i++ {
			upper := strings.ToUpper(args[i])
			if upper == "WITHSCORES" {
				withScores = true
			} else if upper == "LIMIT" && i+2 < len(args) {
				offset, _ = strconv.ParseInt(args[i+1], 10, 64)
				count, _ = strconv.ParseInt(args[i+2], 10, 64)
				i += 2
			}
		}
		items := s.sortedZSet(key, rev)
		var matched []zItem
		for _, it := range items {
			if matchScoreBound(it.score, minStr, true) && matchScoreBound(it.score, maxStr, false) {
				matched = append(matched, it)
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
		out := make([]string, 0, len(matched)*2)
		for _, it := range matched {
			out = append(out, respBulk(it.member))
			if withScores {
				out = append(out, respBulk(strconv.FormatFloat(it.score, 'f', -1, 64)))
			}
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

	case "SPUBLISH":
		ch, payload := args[1], args[2]
		s.pubsubMu.Lock()
		s.SPublishCalls.Add(1)
		subs := append([]net.Conn(nil), s.spubsubSub[ch]...)
		s.pubsubMu.Unlock()
		msg := fmt.Sprintf("*3\r\n$8\r\nsmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(ch), ch, len(payload), payload)
		for _, c := range subs {
			_, _ = c.Write([]byte(msg))
		}
		return respInt(int64(len(subs)))

	case "EVALSHA":
		if ferr, ok := s.failCmds["EVAL"]; ok && ferr != nil {
			return respErr(ferr.Error())
		}
		if len(args) > 3 {
			if ferr, exists := s.failKeys[args[3]]; exists && ferr != nil {
				return respErr(ferr.Error())
			}
		}
		return "-NOSCRIPT No matching script. Please use EVAL.\r\n"

	case "SCRIPT":
		if len(args) >= 3 && strings.ToUpper(args[1]) == "LOAD" {
			return respBulk("fakesha")
		}
		return respSimple("OK")

	case "EVAL":
		return s.execEval(args)

	default:
		return respSimple("OK")
	}
}

func (s *FakeRedisServer) execEval(args []string) string {
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
		if len(argv) >= 4 {
			if ttlSec, err := strconv.ParseInt(argv[3], 10, 64); err == nil && ttlSec > 0 {
				s.ttls[key] = time.Duration(ttlSec) * time.Second
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

	// 4. releaseCronLockScript
	if strings.Contains(script, "GET") && strings.Contains(script, "DEL") && len(keys) == 1 && len(argv) == 1 {
		key := keys[0]
		if s.strings[key] == argv[0] {
			delete(s.strings, key)
			delete(s.ttls, key)
			return respInt(1)
		}
		return respInt(0)
	}

	return respNil()
}

type zItem struct {
	member string
	score  float64
}

func (s *FakeRedisServer) sortedZSet(key string, desc bool) []zItem {
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

func toKilometers(val float64, unit string) float64 {
	switch strings.ToLower(unit) {
	case "m":
		return val / 1000.0
	case "mi":
		return val * 1.60934
	case "ft":
		return val * 0.0003048
	default:
		return val
	}
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0088
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

func checkCrossSlot(cmd string, args []string) string {
	var keys []string
	switch cmd {
	case "MGET", "DEL", "EXISTS", "SINTER", "PFCOUNT":
		if len(args) > 2 {
			keys = args[1:]
		}
	case "BLPOP", "BRPOP":
		// BLPOP key [key ...] timeout
		if len(args) > 3 {
			keys = args[1 : len(args)-1]
		}
	case "EVAL", "EVALSHA":
		if len(args) >= 4 {
			numKeys, err := strconv.Atoi(args[2])
			if err == nil && numKeys > 1 && len(args) >= 3+numKeys {
				keys = args[3 : 3+numKeys]
			}
		}
	case "ZUNIONSTORE":
		if len(args) >= 4 {
			numKeys, err := strconv.Atoi(args[2])
			if err == nil && len(args) >= 3+numKeys {
				keys = append([]string{args[1]}, args[3:3+numKeys]...)
			}
		}
	}
	if len(keys) <= 1 {
		return ""
	}
	firstSlot := redisClusterSlot(keys[0])
	for _, k := range keys[1:] {
		if redisClusterSlot(k) != firstSlot {
			return "-CROSSSLOT Keys in request don't hash to the same slot\r\n"
		}
	}
	return ""
}

// redisClusterSlot implémente l'algorithme officiel CRC16-XMODEM mod 16384 avec support des hash tags {tag}.
func redisClusterSlot(key string) uint16 {
	s := key
	if start := strings.IndexByte(key, '{'); start != -1 {
		if end := strings.IndexByte(key[start+1:], '}'); end > 0 {
			s = key[start+1 : start+1+end]
		}
	}
	var crc uint16
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for j := 0; j < 8; j++ {
			if (crc & 0x8000) != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc & 16383
}
