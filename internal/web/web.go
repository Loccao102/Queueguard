package web

import (
	"bytes"
	_ "embed"
	"html/template"
	"os"
)

//go:embed waiting_room.html
var WaitingRoomHTML []byte

//go:embed admin.html
var AdminHTML []byte

// WaitingRoomData holds dynamic whitelabel branding attributes for rendering the waiting room.
type WaitingRoomData struct {
	EventTitle   string
	BrandLogoURL string
	ThemeColor   string
	Announcement string
	RoomID       string
	RoomName     string
}

// RenderWaitingRoom compiles and executes the waiting room template with custom branding.
// If customTemplatePath is provided and points to an existing file, it will be loaded instead.
func RenderWaitingRoom(customTemplatePath string, data WaitingRoomData) ([]byte, error) {
	tmplBytes := WaitingRoomHTML
	if customTemplatePath != "" {
		if content, err := os.ReadFile(customTemplatePath); err == nil && len(content) > 0 {
			tmplBytes = content
		}
	}

	tmpl, err := template.New("waiting_room").Parse(string(tmplBytes))
	if err != nil {
		return tmplBytes, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return tmplBytes, err
	}

	return buf.Bytes(), nil
}
