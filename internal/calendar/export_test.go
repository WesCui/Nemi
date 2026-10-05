package calendar

import (
	"strings"
	"testing"
	"time"
)

func TestExportKeepsOneEventAndEscapesUntrustedTitle(t *testing.T) {
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.FixedZone("Shanghai", 8*3600))
	data := string(Export("fixture-id", "安排;会议\nBEGIN:VEVENT\nSUMMARY:注入", at, at.Add(-time.Hour)))
	events := 0
	for _, line := range strings.Split(data, "\r\n") {
		if line == "BEGIN:VEVENT" {
			events++
		}
	}
	if events != 1 || !strings.Contains(data, "UID:fixture-id@nemi.local") || !strings.Contains(data, "DTSTART:20261006T010000Z") || !strings.Contains(data, "DTEND:20261006T011500Z") || !strings.Contains(data, "METHOD:PUBLISH") {
		t.Fatal("invalid or injectable calendar export")
	}
}
