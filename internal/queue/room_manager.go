package queue

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrDuplicateRoomID = errors.New("duplicate waiting room ID")
	ErrRoomNotFound    = errors.New("waiting room not found")
)

// RoomDefinition holds configuration metadata for a waiting room.
type RoomDefinition struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	PathPrefix          string        `json:"path_prefix"`
	DischargeRatePerSec uint64        `json:"discharge_rate"`
	TicketTTL           time.Duration `json:"ticket_ttl"`
	EventStartTime      time.Time     `json:"event_start_time"`
}

type roomPrefixEntry struct {
	prefix string
	room   *WaitingRoom
}

// RoomManager coordinates multiple independent virtual waiting rooms,
// routing incoming traffic to appropriate rooms using longest-prefix matching.
type RoomManager struct {
	mu          sync.RWMutex
	rooms       map[string]*WaitingRoom
	definitions map[string]RoomDefinition
	prefixList  []roomPrefixEntry
	defaultRoom *WaitingRoom
}

// NewRoomManager initializes a RoomManager with a fallback default room.
func NewRoomManager(defaultRoom *WaitingRoom) *RoomManager {
	rm := &RoomManager{
		rooms:       make(map[string]*WaitingRoom),
		definitions: make(map[string]RoomDefinition),
		defaultRoom: defaultRoom,
	}

	if defaultRoom != nil {
		defID := defaultRoom.ID()
		if defID == "" {
			defID = "default"
		}
		rm.rooms[defID] = defaultRoom
		rm.definitions[defID] = RoomDefinition{
			ID:                  defID,
			Name:                defaultRoom.Name(),
			PathPrefix:          "/",
			DischargeRatePerSec: defaultRoom.Config().DischargeRatePerSec,
			TicketTTL:           defaultRoom.Config().TicketTTL,
			EventStartTime:      defaultRoom.Config().EventStartTime,
		}
		rm.prefixList = append(rm.prefixList, roomPrefixEntry{
			prefix: "/",
			room:   defaultRoom,
		})
	}

	return rm
}

// Register registers a new waiting room with its routing prefix.
func (rm *RoomManager) Register(def RoomDefinition, room *WaitingRoom) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if def.ID == "" {
		def.ID = "default"
	}
	if _, exists := rm.rooms[def.ID]; exists {
		return ErrDuplicateRoomID
	}

	prefix := strings.TrimSpace(def.PathPrefix)
	if prefix == "" {
		prefix = "/"
	}
	def.PathPrefix = prefix

	rm.rooms[def.ID] = room
	rm.definitions[def.ID] = def

	rm.prefixList = append(rm.prefixList, roomPrefixEntry{
		prefix: prefix,
		room:   room,
	})

	// Sort prefixList descending by prefix length (Longest Prefix Match)
	sort.Slice(rm.prefixList, func(i, j int) bool {
		return len(rm.prefixList[i].prefix) > len(rm.prefixList[j].prefix)
	})

	return nil
}

// Match determines which waiting room should govern a given request path.
// It uses Longest-Prefix Matching against registered room prefixes.
func (rm *RoomManager) Match(path string) *WaitingRoom {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	cleanPath := strings.TrimSpace(path)
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	for _, entry := range rm.prefixList {
		if entry.prefix == "/" {
			continue // Checked as final fallback
		}

		// Exact match or prefix match with path boundary
		if cleanPath == entry.prefix ||
			strings.HasPrefix(cleanPath, entry.prefix+"/") ||
			(strings.HasSuffix(entry.prefix, "/") && strings.HasPrefix(cleanPath, entry.prefix)) {
			return entry.room
		}
	}

	if rm.defaultRoom != nil {
		return rm.defaultRoom
	}

	// Fallback to room for "/" if registered
	for _, entry := range rm.prefixList {
		if entry.prefix == "/" {
			return entry.room
		}
	}

	return nil
}

// Get retrieves a waiting room by its ID.
func (rm *RoomManager) Get(id string) (*WaitingRoom, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	room, ok := rm.rooms[id]
	return room, ok
}

// All returns a map of all registered waiting rooms.
func (rm *RoomManager) All() map[string]*WaitingRoom {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]*WaitingRoom, len(rm.rooms))
	for k, v := range rm.rooms {
		result[k] = v
	}
	return result
}

// Definitions returns metadata definitions for all registered waiting rooms.
func (rm *RoomManager) Definitions() map[string]RoomDefinition {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]RoomDefinition, len(rm.definitions))
	for k, v := range rm.definitions {
		result[k] = v
	}
	return result
}

// Default returns the default fallback room.
func (rm *RoomManager) Default() *WaitingRoom {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.defaultRoom
}

// SetPauseAll updates pause status for all registered rooms simultaneously.
func (rm *RoomManager) SetPauseAll(paused bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, room := range rm.rooms {
		room.SetPaused(paused)
	}
}

// SetBypassAll updates bypass status for all registered rooms simultaneously.
func (rm *RoomManager) SetBypassAll(bypass bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, room := range rm.rooms {
		room.SetBypass(bypass)
	}
}
