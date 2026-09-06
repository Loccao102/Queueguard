package queue

import "sync/atomic"

// SequenceController manages monotonic sequence numbers for the waiting room turnstile.
// It tracks the next ticket to issue and the highest ticket number currently admitted.
// All operations are lock-free and thread-safe using CPU atomic instructions.
type SequenceController struct {
	lastIssued uint64
	admitted   uint64
}

// NewSequenceController initializes a new sequence controller starting at ticket #0.
func NewSequenceController() *SequenceController {
	return &SequenceController{
		lastIssued: 0,
		admitted:   0,
	}
}

// NextTicket atomically issues the next incremental ticket number.
// Executes in single-digit nanoseconds with zero lock contention.
func (sc *SequenceController) NextTicket() uint64 {
	return atomic.AddUint64(&sc.lastIssued, 1)
}

// LastIssued returns the most recently issued ticket number.
func (sc *SequenceController) LastIssued() uint64 {
	return atomic.LoadUint64(&sc.lastIssued)
}

// Admitted returns the highest ticket number that has been granted admission.
func (sc *SequenceController) Admitted() uint64 {
	return atomic.LoadUint64(&sc.admitted)
}

// AdvanceAdmission atomically increments the admitted threshold by up to 'count' tickets,
// without exceeding the last issued ticket. Returns the new admitted threshold.
func (sc *SequenceController) AdvanceAdmission(count uint64) (newAdmitted uint64, advanced uint64) {
	for {
		currentAdmitted := atomic.LoadUint64(&sc.admitted)
		lastIssued := atomic.LoadUint64(&sc.lastIssued)

		if currentAdmitted >= lastIssued {
			return currentAdmitted, 0
		}

		target := currentAdmitted + count
		if target > lastIssued {
			target = lastIssued
		}

		if atomic.CompareAndSwapUint64(&sc.admitted, currentAdmitted, target) {
			return target, target - currentAdmitted
		}
	}
}

// SetAdmitted forces the admitted threshold to a specific value (e.g., during emergency flush or reset).
func (sc *SequenceController) SetAdmitted(val uint64) {
	atomic.StoreUint64(&sc.admitted, val)
}

// QueueDepth returns the current number of people waiting in line.
func (sc *SequenceController) QueueDepth() uint64 {
	lastIssued := atomic.LoadUint64(&sc.lastIssued)
	admitted := atomic.LoadUint64(&sc.admitted)
	if lastIssued > admitted {
		return lastIssued - admitted
	}
	return 0
}

// PositionOf calculates how many people are ahead of a given ticket number.
// If ticket <= admitted, the ticket is already admitted (returns 0).
func (sc *SequenceController) PositionOf(ticket uint64) uint64 {
	admitted := atomic.LoadUint64(&sc.admitted)
	if ticket <= admitted {
		return 0
	}
	return ticket - admitted
}

// Reset clears the sequence counters back to 0.
func (sc *SequenceController) Reset() {
	atomic.StoreUint64(&sc.lastIssued, 0)
	atomic.StoreUint64(&sc.admitted, 0)
}
