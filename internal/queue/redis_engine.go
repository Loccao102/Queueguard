package queue

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RedisEngine implements Engine using a Redis instance for distributed turnstile synchronization.
type RedisEngine struct {
	addr   string
	prefix string
	mu     sync.Mutex
	conn   net.Conn
	reader *bufio.Reader
}

// NewRedisEngine creates a new distributed Redis turnstile engine.
func NewRedisEngine(redisAddr string, prefix string) (*RedisEngine, error) {
	if prefix == "" {
		prefix = "queueguard"
	}
	re := &RedisEngine{
		addr:   redisAddr,
		prefix: prefix,
	}

	conn, err := re.getConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redis at %s: %w", redisAddr, err)
	}
	re.conn = conn
	re.reader = bufio.NewReader(conn)

	return re, nil
}

func (re *RedisEngine) getConn() (net.Conn, error) {
	if re.conn != nil {
		return re.conn, nil
	}
	conn, err := net.DialTimeout("tcp", re.addr, 3*time.Second)
	if err != nil {
		return nil, err
	}
	re.conn = conn
	re.reader = bufio.NewReader(conn)
	return conn, nil
}

func (re *RedisEngine) execCommand(args ...string) (any, error) {
	re.mu.Lock()
	defer re.mu.Unlock()

	conn, err := re.getConn()
	if err != nil {
		return nil, err
	}

	// Format RESP array: *<count>\r\n$<len>\r\n<arg>\r\n...
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, arg := range args {
		buf.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(buf.String())); err != nil {
		re.conn = nil
		return nil, err
	}

	return re.readResp()
}

func (re *RedisEngine) readResp() (any, error) {
	line, err := re.reader.ReadString('\n')
	if err != nil {
		re.conn = nil
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, fmt.Errorf("empty resp line")
	}

	switch line[0] {
	case '+': // Simple string
		return line[1:], nil
	case '-': // Error
		return nil, fmt.Errorf("redis error: %s", line[1:])
	case ':': // Integer
		return strconv.ParseInt(line[1:], 10, 64)
	case '$': // Bulk string
		length, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if length == -1 {
			return nil, nil // nil bulk string
		}
		body := make([]byte, length+2)
		var totalRead int
		for totalRead < length+2 {
			n, err := re.reader.Read(body[totalRead:])
			if err != nil {
				re.conn = nil
				return nil, err
			}
			totalRead += n
		}
		return string(body[:length]), nil
	default:
		return line, nil
	}
}

// NextTicket atomically increments and returns the next ticket in Redis.
func (re *RedisEngine) NextTicket() uint64 {
	key := re.prefix + ":last_issued"
	res, err := re.execCommand("INCR", key)
	if err != nil {
		return 0
	}
	if n, ok := res.(int64); ok && n > 0 {
		return uint64(n)
	}
	return 0
}

// LastIssued gets the highest issued ticket.
func (re *RedisEngine) LastIssued() uint64 {
	key := re.prefix + ":last_issued"
	res, err := re.execCommand("GET", key)
	if err != nil || res == nil {
		return 0
	}
	if str, ok := res.(string); ok {
		n, _ := strconv.ParseUint(str, 10, 64)
		return n
	}
	return 0
}

// Admitted gets the current admitted threshold from Redis.
func (re *RedisEngine) Admitted() uint64 {
	key := re.prefix + ":admitted"
	res, err := re.execCommand("GET", key)
	if err != nil || res == nil {
		return 0
	}
	if str, ok := res.(string); ok {
		n, _ := strconv.ParseUint(str, 10, 64)
		return n
	}
	return 0
}

// AdvanceAdmission uses an atomic Lua script to advance admitted threshold without exceeding lastIssued.
func (re *RedisEngine) AdvanceAdmission(count uint64) (uint64, uint64) {
	script := `
		local admitted = tonumber(redis.call('GET', KEYS[1]) or 0)
		local last_issued = tonumber(redis.call('GET', KEYS[2]) or 0)
		local count = tonumber(ARGV[1])
		if admitted >= last_issued then
			return {admitted, 0}
		end
		local target = admitted + count
		if target > last_issued then
			target = last_issued
		end
		redis.call('SET', KEYS[1], target)
		return {target, target - admitted}
	`
	keyAdmitted := re.prefix + ":admitted"
	keyLastIssued := re.prefix + ":last_issued"

	res, err := re.execCommand("EVAL", script, "2", keyAdmitted, keyLastIssued, strconv.FormatUint(count, 10))
	if err != nil {
		// Fallback simple increment
		curr := re.Admitted()
		last := re.LastIssued()
		if curr >= last {
			return curr, 0
		}
		target := curr + count
		if target > last {
			target = last
		}
		re.SetAdmitted(target)
		return target, target - curr
	}

	if arr, ok := res.([]any); ok && len(arr) == 2 {
		t, _ := arr[0].(int64)
		adv, _ := arr[1].(int64)
		return uint64(t), uint64(adv)
	}

	return re.Admitted(), 0
}

// SetAdmitted forces admitted threshold in Redis.
func (re *RedisEngine) SetAdmitted(val uint64) {
	key := re.prefix + ":admitted"
	_, _ = re.execCommand("SET", key, strconv.FormatUint(val, 10))
}

// SetLastIssued forces last_issued in Redis.
func (re *RedisEngine) SetLastIssued(val uint64) {
	key := re.prefix + ":last_issued"
	_, _ = re.execCommand("SET", key, strconv.FormatUint(val, 10))
}

// Reset clears both keys in Redis.
func (re *RedisEngine) Reset() {
	keyAdmitted := re.prefix + ":admitted"
	keyLastIssued := re.prefix + ":last_issued"
	_, _ = re.execCommand("MSET", keyAdmitted, "0", keyLastIssued, "0")
}

// QueueDepth calculates people waiting in Redis.
func (re *RedisEngine) QueueDepth() uint64 {
	last := re.LastIssued()
	admitted := re.Admitted()
	if last > admitted {
		return last - admitted
	}
	return 0
}

// PositionOf calculates position in Redis.
func (re *RedisEngine) PositionOf(ticket uint64) uint64 {
	admitted := re.Admitted()
	if ticket <= admitted {
		return 0
	}
	return ticket - admitted
}

// Close closes the Redis socket.
func (re *RedisEngine) Close() error {
	re.mu.Lock()
	defer re.mu.Unlock()
	if re.conn != nil {
		err := re.conn.Close()
		re.conn = nil
		return err
	}
	return nil
}

var _ Engine = (*RedisEngine)(nil)
