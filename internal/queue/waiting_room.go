package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
)

// SessionStatus represents the state of a user in the waiting room.
type SessionStatus string

const (
	StatusWaiting  SessionStatus = "waiting"
	StatusAdmitted SessionStatus = "admitted"
	StatusExpired  SessionStatus = "expired"
)

// Session holds state for a user queued in the waiting room.
type Session struct {
	ID             string        `json:"id"`
	TicketNumber   uint64        `json:"ticket_number"`
	EnqueuedAt     time.Time     `json:"enqueued_at"`
	LastHeartbeat  int64         `json:"last_heartbeat"` // unix milli
	Status         SessionStatus `json:"status"`
	AdmissionToken string        `json:"-"`
}

// Config defines waiting room operational limits and thresholds.
type Config struct {
	DischargeRatePerSec uint64        // Number of users admitted per second
	MaxQueueCapacity    uint64        // Maximum allowed in waiting room
	TicketTTL           time.Duration // Time an admitted ticket remains valid on the origin
	SessionIdleTimeout  time.Duration // Time before an inactive tab is considered abandoned
}

// DefaultConfig provides recommended baseline settings.
func DefaultConfig() Config {
	return Config{
		DischargeRatePerSec: 20,
		MaxQueueCapacity:    100000,
		TicketTTL:           10 * time.Minute,
		SessionIdleTimeout:  60 * time.Second,
	}
}

// WaitingRoom orchestrates user queueing, atomic turnstile sequencing,
// token-bucket discharge, and SSE position broadcasting.
type WaitingRoom struct {
	config   Config
	sequence *SequenceController
	signer   *crypto.Signer

	// sessions stores active queued sessions (sessionID -> *Session)
	sessions sync.Map

	// broadcast channels for live SSE subscribers
	subscribersMu sync.RWMutex
	subscribers   map[chan struct{}]struct{}

	// Operational controls (Atomic flags)
	paused uint32 // 1 = emergency stop all admissions
	bypass uint32 // 1 = bypass waiting room entirely (normal hours)
}

// NewWaitingRoom constructs an initialized WaitingRoom.
func NewWaitingRoom(cfg Config, signer *crypto.Signer) *WaitingRoom {
	if cfg.DischargeRatePerSec == 0 {
		cfg.DischargeRatePerSec = 20
	}
	if cfg.SessionIdleTimeout == 0 {
		cfg.SessionIdleTimeout = 60 * time.Second
	}

	return &WaitingRoom{
		config:      cfg,
		sequence:    NewSequenceController(),
		signer:      signer,
		subscribers: make(map[chan struct{}]struct{}),
	}
}

// Enroll checks if the session already holds a valid queue ticket.
// If it does, it returns the existing ticket (surviving F5 page refreshes).
// Otherwise, it atomically assigns the next ticket in the sequence.
func (wr *WaitingRoom) Enroll(sessionID string) (*Session, bool) {
	now := time.Now()

	// Check if already in queue
	if val, ok := wr.sessions.Load(sessionID); ok {
		sess := val.(*Session)
		atomic.StoreInt64(&sess.LastHeartbeat, now.UnixMilli())
		return sess, false
	}

	// Atomically grab next ticket number
	ticketNum := wr.sequence.NextTicket()
	sess := &Session{
		ID:            sessionID,
		TicketNumber:  ticketNum,
		EnqueuedAt:    now,
		LastHeartbeat: now.UnixMilli(),
		Status:        StatusWaiting,
	}

	wr.sessions.Store(sessionID, sess)
	return sess, true
}

// Heartbeat refreshes the last active timestamp of a waiting session.
func (wr *WaitingRoom) Heartbeat(sessionID string) bool {
	if val, ok := wr.sessions.Load(sessionID); ok {
		sess := val.(*Session)
		atomic.StoreInt64(&sess.LastHeartbeat, time.Now().UnixMilli())
		return true
	}
	return false
}

// CheckStatus queries the current standing of a session.
// Returns admitted (bool), position ahead (uint64), estimated wait seconds (int64), and admission token if admitted.
func (wr *WaitingRoom) CheckStatus(sessionID string) (bool, uint64, int64, string, error) {
	// If bypass mode is active, everyone is admitted immediately
	if atomic.LoadUint32(&wr.bypass) == 1 {
		token, _, err := wr.signer.Issue(sessionID, 0)
		return true, 0, 0, token, err
	}

	val, ok := wr.sessions.Load(sessionID)
	if !ok {
		return false, 0, 0, "", fmt.Errorf("session not found in waiting room")
	}

	sess := val.(*Session)
	atomic.StoreInt64(&sess.LastHeartbeat, time.Now().UnixMilli())

	admittedThreshold := wr.sequence.Admitted()

	// Check if this ticket has been reached by the admission turnstile
	if sess.TicketNumber <= admittedThreshold && atomic.LoadUint32(&wr.paused) == 0 {
		sess.Status = StatusAdmitted
		if sess.AdmissionToken == "" {
			token, _, err := wr.signer.Issue(sess.ID, sess.TicketNumber)
			if err != nil {
				return false, 0, 0, "", err
			}
			sess.AdmissionToken = token
		}
		return true, 0, 0, sess.AdmissionToken, nil
	}

	// Still waiting
	position := sess.TicketNumber - admittedThreshold
	rate := atomic.LoadUint64(&wr.config.DischargeRatePerSec)
	if rate == 0 {
		rate = 1
	}

	estSeconds := int64(position / rate)
	if estSeconds == 0 {
		estSeconds = 1
	}

	return false, position, estSeconds, "", nil
}

// StartDischargeWorker runs a background ticker that steadily admits queued users
// according to the configured DischargeRatePerSec.
func (wr *WaitingRoom) StartDischargeWorker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	pruneTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer pruneTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			// If paused, do not advance turnstile
			if atomic.LoadUint32(&wr.paused) == 1 {
				continue
			}

			rate := atomic.LoadUint64(&wr.config.DischargeRatePerSec)
			if rate > 0 {
				_, advanced := wr.sequence.AdvanceAdmission(rate)
				if advanced > 0 {
					wr.notifySubscribers()
				}
			}

		case <-pruneTicker.C:
			wr.pruneStaleSessions()
		}
	}
}

// Subscribe returns a channel that receives a signal whenever the queue advances.
func (wr *WaitingRoom) Subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	wr.subscribersMu.Lock()
	wr.subscribers[ch] = struct{}{}
	wr.subscribersMu.Unlock()

	unsubscribe := func() {
		wr.subscribersMu.Lock()
		delete(wr.subscribers, ch)
		wr.subscribersMu.Unlock()
	}

	return ch, unsubscribe
}

func (wr *WaitingRoom) notifySubscribers() {
	wr.subscribersMu.RLock()
	defer wr.subscribersMu.RUnlock()

	for ch := range wr.subscribers {
		select {
		case ch <- struct{}{}:
		default:
			// Non-blocking write so slow consumers don't block the dispatcher
		}
	}
}

func (wr *WaitingRoom) pruneStaleSessions() {
	nowMilli := time.Now().UnixMilli()
	timeoutMilli := wr.config.SessionIdleTimeout.Milliseconds()

	wr.sessions.Range(func(key, value any) bool {
		sess := value.(*Session)
		lastHeartbeat := atomic.LoadInt64(&sess.LastHeartbeat)
		if nowMilli-lastHeartbeat > timeoutMilli {
			wr.sessions.Delete(key)
		}
		return true
	})
}

// Operational Management API controls
func (wr *WaitingRoom) SetDischargeRate(rate uint64) {
	atomic.StoreUint64(&wr.config.DischargeRatePerSec, rate)
}

func (wr *WaitingRoom) GetDischargeRate() uint64 {
	return atomic.LoadUint64(&wr.config.DischargeRatePerSec)
}

func (wr *WaitingRoom) SetPaused(paused bool) {
	if paused {
		atomic.StoreUint32(&wr.paused, 1)
	} else {
		atomic.StoreUint32(&wr.paused, 0)
	}
}

func (wr *WaitingRoom) IsPaused() bool {
	return atomic.LoadUint32(&wr.paused) == 1
}

func (wr *WaitingRoom) SetBypass(bypass bool) {
	if bypass {
		atomic.StoreUint32(&wr.bypass, 1)
	} else {
		atomic.StoreUint32(&wr.bypass, 0)
	}
}

func (wr *WaitingRoom) IsBypass() bool {
	return atomic.LoadUint32(&wr.bypass) == 1
}

func (wr *WaitingRoom) Sequence() *SequenceController {
	return wr.sequence
}

// ActiveSessionsCount counts the number of tracked sessions in memory.
func (wr *WaitingRoom) ActiveSessionsCount() int {
	count := 0
	wr.sessions.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// SubscribersCount counts the active SSE connections listening to the queue.
func (wr *WaitingRoom) SubscribersCount() int {
	wr.subscribersMu.RLock()
	defer wr.subscribersMu.RUnlock()
	return len(wr.subscribers)
}

// Reset flushes all waiting sessions and resets the turnstile sequence counter.
func (wr *WaitingRoom) Reset() {
	wr.sequence.Reset()
	wr.sessions.Range(func(key, _ any) bool {
		wr.sessions.Delete(key)
		return true
	})
	wr.notifySubscribers()
}
