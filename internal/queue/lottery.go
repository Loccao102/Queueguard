package queue

import (
	"crypto/rand"
	"math/big"
	"sync"
	"time"
)

// PreQueueManager collects users before the official event start time and
// assigns randomized ticket numbers via a Fair Lottery Shuffle when the event starts.
type PreQueueManager struct {
	mu         sync.RWMutex
	startTime  time.Time
	active     bool
	completed  bool
	sessionIDs []string
	sessionSet map[string]struct{}
}

// NewPreQueueManager creates a pre-queue manager.
// If startTime is in the future, pre-queue mode is automatically active.
func NewPreQueueManager(startTime time.Time) *PreQueueManager {
	isActive := !startTime.IsZero() && time.Now().Before(startTime)
	return &PreQueueManager{
		startTime:  startTime,
		active:     isActive,
		completed:  !isActive,
		sessionIDs: make([]string, 0),
		sessionSet: make(map[string]struct{}),
	}
}

// IsActive returns true if the pre-queue countdown is currently running.
func (pq *PreQueueManager) IsActive() bool {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	if !pq.active || pq.completed {
		return false
	}
	return time.Now().Before(pq.startTime)
}

// StartTime returns the scheduled event start time.
func (pq *PreQueueManager) StartTime() time.Time {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return pq.startTime
}

// SetStartTime dynamically updates or enables the pre-queue event start time.
func (pq *PreQueueManager) SetStartTime(t time.Time) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.startTime = t
	if !t.IsZero() && time.Now().Before(t) {
		pq.active = true
		pq.completed = false
	} else {
		pq.active = false
		pq.completed = true
	}
}

// SecondsUntilStart returns remaining seconds until event start (or 0 if already started).
func (pq *PreQueueManager) SecondsUntilStart() int64 {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	if !pq.active || pq.completed {
		return 0
	}
	diff := time.Until(pq.startTime)
	if diff <= 0 {
		return 0
	}
	return int64(diff.Seconds())
}

// Enroll adds a session into the pre-queue pool.
func (pq *PreQueueManager) Enroll(sessionID string) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if !pq.active || pq.completed {
		return false
	}

	if _, exists := pq.sessionSet[sessionID]; exists {
		return true // already enrolled in pre-queue
	}

	pq.sessionSet[sessionID] = struct{}{}
	pq.sessionIDs = append(pq.sessionIDs, sessionID)
	return true
}

// Count returns the number of participants currently waiting in the pre-queue.
func (pq *PreQueueManager) Count() int {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return len(pq.sessionIDs)
}

// ShouldTriggerLottery checks whether start time has passed and lottery hasn't executed yet.
func (pq *PreQueueManager) ShouldTriggerLottery() bool {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return pq.active && !pq.completed && !time.Now().Before(pq.startTime)
}

// ExecuteFairLottery performs a cryptographic Fisher-Yates shuffle on all collected
// pre-queue participants and assigns each a randomized ticket from 1 to N.
func (pq *PreQueueManager) ExecuteFairLottery(seq Engine, sessions *sync.Map) int {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if !pq.active || pq.completed {
		return 0
	}

	n := len(pq.sessionIDs)
	if n == 0 {
		pq.completed = true
		return 0
	}

	// Fisher-Yates shuffle using cryptographically secure random integers
	shuffled := make([]string, n)
	copy(shuffled, pq.sessionIDs)

	for i := n - 1; i > 0; i-- {
		bigJ, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		var j int
		if err != nil {
			// Fallback in case of crypto error (rare)
			j = int(time.Now().UnixNano() % int64(i+1))
		} else {
			j = int(bigJ.Int64())
		}
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	// Assign ticket numbers 1..n to the randomly shuffled sessions
	for idx, sID := range shuffled {
		assignedTicket := uint64(idx + 1)
		if val, ok := sessions.Load(sID); ok {
			sess := val.(*Session)
			sess.TicketNumber = assignedTicket
		}
	}

	// Advance turnstile counter so subsequent normal arrivals get ticket n+1, n+2...
	seq.SetLastIssued(uint64(n))

	pq.completed = true
	return n
}
