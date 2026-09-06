package queue

// Engine defines the contract for turnstile sequencing operations,
// supporting both in-memory CAS and distributed cluster engines (e.g., Redis).
type Engine interface {
	NextTicket() uint64
	LastIssued() uint64
	Admitted() uint64
	AdvanceAdmission(count uint64) (newAdmitted uint64, advanced uint64)
	SetAdmitted(val uint64)
	SetLastIssued(val uint64)
	Reset()
	QueueDepth() uint64
	PositionOf(ticket uint64) uint64
}

// Ensure SequenceController implements Engine
var _ Engine = (*SequenceController)(nil)
