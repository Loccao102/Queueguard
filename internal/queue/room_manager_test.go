package queue

import (
	"testing"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
)

func TestRoomManagerRouting(t *testing.T) {
	signer := crypto.NewSigner("test-secret", 5*time.Minute)

	defaultRoom := NewWaitingRoom(Config{RoomID: "default", Name: "Default Lounge"}, signer)
	vipRoom := NewWaitingRoom(Config{RoomID: "vip", Name: "VIP Room", DischargeRatePerSec: 5}, signer)
	genRoom := NewWaitingRoom(Config{RoomID: "general", Name: "General Room", DischargeRatePerSec: 50}, signer)

	rm := NewRoomManager(defaultRoom)

	err := rm.Register(RoomDefinition{
		ID:         "vip",
		Name:       "VIP Room",
		PathPrefix: "/tickets/vip",
	}, vipRoom)
	if err != nil {
		t.Fatalf("failed to register VIP room: %v", err)
	}

	err = rm.Register(RoomDefinition{
		ID:         "general",
		Name:       "General Room",
		PathPrefix: "/tickets",
	}, genRoom)
	if err != nil {
		t.Fatalf("failed to register General room: %v", err)
	}

	// 1. Longest prefix: /tickets/vip/checkout should match vipRoom
	matched := rm.Match("/tickets/vip/checkout")
	if matched != vipRoom {
		t.Fatalf("expected /tickets/vip/checkout to match VIP room, got %s", matched.ID())
	}

	// 2. /tickets/general should match genRoom (/tickets prefix)
	matched = rm.Match("/tickets/general")
	if matched != genRoom {
		t.Fatalf("expected /tickets/general to match General room, got %s", matched.ID())
	}

	// 3. /about should fallback to defaultRoom
	matched = rm.Match("/about")
	if matched != defaultRoom {
		t.Fatalf("expected /about to match default room, got %s", matched.ID())
	}

	// 4. Duplicate ID registration should fail
	err = rm.Register(RoomDefinition{
		ID:         "vip",
		Name:       "VIP Duplicate",
		PathPrefix: "/duplicate",
	}, vipRoom)
	if err != ErrDuplicateRoomID {
		t.Fatalf("expected ErrDuplicateRoomID, got %v", err)
	}

	// 5. Check Get and All
	if r, ok := rm.Get("vip"); !ok || r != vipRoom {
		t.Fatal("expected to retrieve VIP room by ID")
	}
	if len(rm.All()) != 3 {
		t.Fatalf("expected 3 registered rooms, got %d", len(rm.All()))
	}

	// 6. Test SetPauseAll
	rm.SetPauseAll(true)
	if !defaultRoom.IsPaused() || !vipRoom.IsPaused() || !genRoom.IsPaused() {
		t.Fatal("expected all rooms to be paused")
	}

	rm.SetPauseAll(false)
	if defaultRoom.IsPaused() || vipRoom.IsPaused() || genRoom.IsPaused() {
		t.Fatal("expected all rooms to be unpaused")
	}
}
