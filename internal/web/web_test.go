package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderWaitingRoom_DefaultTemplate(t *testing.T) {
	data := WaitingRoomData{
		EventTitle:   "Taylor Swift Concert 2026",
		BrandLogoURL: "https://example.com/taylor-logo.png",
		ThemeColor:   "#8b5cf6",
		Announcement: "Mỗi tài khoản tối đa 2 vé",
		RoomID:       "vip",
		RoomName:     "Khu Vực VIP Lounge",
	}

	html, err := RenderWaitingRoom("", data)
	if err != nil {
		t.Fatalf("expected rendering to succeed: %v", err)
	}

	rendered := string(html)
	if !strings.Contains(rendered, "Taylor Swift Concert 2026") {
		t.Error("expected rendered HTML to contain EventTitle")
	}
	if !strings.Contains(rendered, "https://example.com/taylor-logo.png") {
		t.Error("expected rendered HTML to contain BrandLogoURL")
	}
	if !strings.Contains(rendered, "#8b5cf6") {
		t.Error("expected rendered HTML to contain ThemeColor")
	}
	if !strings.Contains(rendered, "Mỗi tài khoản tối đa 2 vé") {
		t.Error("expected rendered HTML to contain Announcement")
	}
	if !strings.Contains(rendered, "Khu Vực VIP Lounge") {
		t.Error("expected rendered HTML to contain RoomName")
	}
}

func TestRenderWaitingRoom_CustomTemplateFile(t *testing.T) {
	tempDir := t.TempDir()
	customFile := filepath.Join(tempDir, "custom.html")
	customContent := "<html><body><h1>{{.EventTitle}}</h1><p>{{.Announcement}}</p></body></html>"
	if err := os.WriteFile(customFile, []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	data := WaitingRoomData{
		EventTitle:   "Custom Title",
		Announcement: "Custom Notice",
	}

	html, err := RenderWaitingRoom(customFile, data)
	if err != nil {
		t.Fatalf("failed to render custom template: %v", err)
	}

	rendered := string(html)
	if !strings.Contains(rendered, "<h1>Custom Title</h1>") || !strings.Contains(rendered, "<p>Custom Notice</p>") {
		t.Errorf("custom template output mismatch: %s", rendered)
	}
}
