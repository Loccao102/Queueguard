package web

import _ "embed"

//go:embed waiting_room.html
var WaitingRoomHTML []byte

//go:embed admin.html
var AdminHTML []byte
