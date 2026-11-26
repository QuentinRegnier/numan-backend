package redis

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redisPre7Stub simule un serveur Redis < 7 rejetant l'option NX de EXPIRE
// tout en acceptant HINCRBY, PFADD et EXPIRE standard (3 arguments).
type redisPre7Stub struct {
	ln              net.Listener
	hincrByCalls    atomic.Int64
	pfAddCalls      atomic.Int64
	expireNXCalls   atomic.Int64
	expirePlainCall atomic.Int64
}

func startRedisPre7Stub(t *testing.T) *redisPre7Stub {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	stub := &redisPre7Stub{ln: ln}
	go stub.acceptLoop()
	t.Cleanup(func() {
		_ = ln.Close()
	})
	return stub
}

func (s *redisPre7Stub) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(conn)
	}
}

func (s *redisPre7Stub) serveConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		args, err := readRESPCommand(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])
		switch cmd {
		case "HELLO":
			_, _ = w.WriteString("-ERR unknown command 'HELLO'\r\n")
		case "CLIENT":
			_, _ = w.WriteString("+OK\r\n")
		case "PING":
			_, _ = w.WriteString("+PONG\r\n")
		case "HINCRBY":
			n := s.hincrByCalls.Add(1)
			_, _ = fmt.Fprintf(w, ":%d\r\n", n)
		case "PFADD":
			s.pfAddCalls.Add(1)
			_, _ = w.WriteString(":1\r\n")
		case "EXPIRE":
			if len(args) >= 4 && strings.EqualFold(args[3], "NX") {
				s.expireNXCalls.Add(1)
				_, _ = w.WriteString("-ERR wrong number of arguments for 'expire' command\r\n")
			} else {
				s.expirePlainCall.Add(1)
				_, _ = w.WriteString(":1\r\n")
			}
		default:
			_, _ = w.WriteString("+OK\r\n")
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "*") {
		return strings.Fields(line), nil
	}
	count, err := strconv.Atoi(line[1:])
	if err != nil || count < 0 {
		return nil, io.EOF
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		lenLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		lenLine = strings.TrimRight(lenLine, "\r\n")
		n, err := strconv.Atoi(strings.TrimPrefix(lenLine, "$"))
		if err != nil || n < 0 {
			return nil, io.EOF
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:n]))
	}
	return args, nil
}

func newStubClient(t *testing.T, addr string) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:       addr,
		Protocol:   2,
		MaxRetries: 0,
	})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func TestRecordCommunityMessageActivityPipeline_ExpireNXFallbackNoDoubleIncrement(t *testing.T) {
	ctx := context.Background()

	t.Run("same client fallback retries only Expire without double HINCRBY or PFADD", func(t *testing.T) {
		stub := startRedisPre7Stub(t)
		client := newStubClient(t, stub.ln.Addr().String())

		msgCol := &Collection{client: client, Prefix: "comm_reco:msg_count", DefaultTTL: time.Hour}
		authorsCol := &Collection{client: client, Prefix: "comm_reco:authors", DefaultTTL: time.Hour}

		err := RecordCommunityMessageActivityPipeline(
			ctx, msgCol, authorsCol, "2026-11-26", "42:2026-11-26", "42", "user_1", time.Hour,
		)
		require.NoError(t, err)

		assert.Equal(t, int64(1), stub.hincrByCalls.Load(), "HINCRBY must execute exactly once (no double increment)")
		assert.Equal(t, int64(1), stub.pfAddCalls.Load(), "PFADD must execute exactly once")
		assert.Equal(t, int64(2), stub.expireNXCalls.Load(), "EXPIRE ... NX attempted once per key")
		assert.Equal(t, int64(2), stub.expirePlainCall.Load(), "fallback plain EXPIRE executed once per errored key")
	})

	t.Run("separate clients route each collection to its own tier without double increment", func(t *testing.T) {
		msgStub := startRedisPre7Stub(t)
		authorsStub := startRedisPre7Stub(t)
		msgClient := newStubClient(t, msgStub.ln.Addr().String())
		authorsClient := newStubClient(t, authorsStub.ln.Addr().String())

		msgCol := &Collection{client: msgClient, Prefix: "comm_reco:msg_count", DefaultTTL: time.Hour}
		authorsCol := &Collection{client: authorsClient, Prefix: "comm_reco:authors", DefaultTTL: time.Hour}

		err := RecordCommunityMessageActivityPipeline(
			ctx, msgCol, authorsCol, "2026-11-26", "42:2026-11-26", "42", "user_1", time.Hour,
		)
		require.NoError(t, err)

		assert.Equal(t, int64(1), msgStub.hincrByCalls.Load())
		assert.Equal(t, int64(0), msgStub.pfAddCalls.Load())
		assert.Equal(t, int64(1), msgStub.expireNXCalls.Load())
		assert.Equal(t, int64(1), msgStub.expirePlainCall.Load())

		assert.Equal(t, int64(0), authorsStub.hincrByCalls.Load())
		assert.Equal(t, int64(1), authorsStub.pfAddCalls.Load())
		assert.Equal(t, int64(1), authorsStub.expireNXCalls.Load())
		assert.Equal(t, int64(1), authorsStub.expirePlainCall.Load())
	})
}
