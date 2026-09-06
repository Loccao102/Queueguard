package queue

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// mockRedisServer provides an in-memory TCP mock responding to basic RESP commands.
type mockRedisServer struct {
	listener net.Listener
	data     map[string]string
	mu       sync.Mutex
	closed   bool
}

func startMockRedis(t *testing.T) *mockRedisServer {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock redis: %v", err)
	}
	server := &mockRedisServer{
		listener: l,
		data:     make(map[string]string),
	}

	go server.serve()
	return server
}

func (s *mockRedisServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleClient(conn)
	}
}

func (s *mockRedisServer) handleClient(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "*") {
			continue
		}

		numArgs, _ := strconv.Atoi(line[1:])
		args := make([]string, 0, numArgs)

		for i := 0; i < numArgs; i++ {
			// Read $<len>
			lenLine, _ := reader.ReadString('\n')
			lenLine = strings.TrimRight(lenLine, "\r\n")
			argLen, _ := strconv.Atoi(lenLine[1:])

			argBytes := make([]byte, argLen+2)
			var readSoFar int
			for readSoFar < argLen+2 {
				n, _ := reader.Read(argBytes[readSoFar:])
				readSoFar += n
			}
			args = append(args, string(argBytes[:argLen]))
		}

		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])
		s.mu.Lock()

		switch cmd {
		case "INCR":
			key := args[1]
			val, _ := strconv.ParseInt(s.data[key], 10, 64)
			val++
			s.data[key] = strconv.FormatInt(val, 10)
			conn.Write([]byte(fmt.Sprintf(":%d\r\n", val)))

		case "GET":
			key := args[1]
			val, ok := s.data[key]
			if !ok {
				conn.Write([]byte("$-1\r\n"))
			} else {
				conn.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(val), val)))
			}

		case "SET":
			key, val := args[1], args[2]
			s.data[key] = val
			conn.Write([]byte("+OK\r\n"))

		case "MSET":
			for i := 1; i < len(args); i += 2 {
				s.data[args[i]] = args[i+1]
			}
			conn.Write([]byte("+OK\r\n"))

		default:
			conn.Write([]byte("+OK\r\n"))
		}

		s.mu.Unlock()
	}
}

func (s *mockRedisServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.listener.Close()
	}
}

func TestRedisEngineMock(t *testing.T) {
	mock := startMockRedis(t)
	defer mock.Close()

	engine, err := NewRedisEngine(mock.listener.Addr().String(), "testguard")
	if err != nil {
		t.Fatalf("failed to connect to mock redis: %v", err)
	}
	defer engine.Close()

	// 1. NextTicket
	t1 := engine.NextTicket()
	if t1 != 1 {
		t.Fatalf("expected ticket 1, got %d", t1)
	}
	t2 := engine.NextTicket()
	if t2 != 2 {
		t.Fatalf("expected ticket 2, got %d", t2)
	}

	// 2. LastIssued
	if engine.LastIssued() != 2 {
		t.Fatalf("expected last issued 2, got %d", engine.LastIssued())
	}

	// 3. SetAdmitted and Admitted
	engine.SetAdmitted(1)
	if engine.Admitted() != 1 {
		t.Fatalf("expected admitted 1, got %d", engine.Admitted())
	}

	// 4. QueueDepth & PositionOf
	if engine.QueueDepth() != 1 {
		t.Fatalf("expected queue depth 1, got %d", engine.QueueDepth())
	}
	if engine.PositionOf(2) != 1 {
		t.Fatalf("expected position of ticket 2 to be 1, got %d", engine.PositionOf(2))
	}
	if engine.PositionOf(1) != 0 {
		t.Fatalf("expected position of ticket 1 to be 0, got %d", engine.PositionOf(1))
	}

	// 5. Reset
	engine.Reset()
	if engine.LastIssued() != 0 || engine.Admitted() != 0 {
		t.Fatalf("expected reset to 0, got last=%d adm=%d", engine.LastIssued(), engine.Admitted())
	}
}
