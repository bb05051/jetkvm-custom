package hidrpc

import (
	"testing"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

func TestTouchscreenReport(t *testing.T) {
	// Bytes as produced by TouchscreenReportMessage.marshal() in ui/src/hooks/hidRpc.ts
	data := []byte{
		byte(TypeTouchscreenReport),
		2,                            // count
		1, 5, 0x12, 0x34, 0x7F, 0xFF, // tip, id, x (BE), y (BE)
		0, 6, 0x00, 0x00, 0x00, 0x01,
	}

	var msg Message
	if err := Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	report, err := msg.TouchscreenReport()
	if err != nil {
		t.Fatal(err)
	}

	want := []usbgadget.TouchContact{
		{ID: 5, Tip: true, X: 0x1234, Y: 0x7FFF},
		{ID: 6, Tip: false, X: 0, Y: 1},
	}
	if len(report.Contacts) != len(want) {
		t.Fatalf("got %d contacts, want %d", len(report.Contacts), len(want))
	}
	for i := range want {
		if report.Contacts[i] != want[i] {
			t.Errorf("contact %d: got %+v, want %+v", i, report.Contacts[i], want[i])
		}
	}
}

func TestTouchscreenReportInvalid(t *testing.T) {
	cases := map[string][]byte{
		"empty":          {},
		"too many":       {usbgadget.TouchscreenMaxContacts + 1},
		"short":          {1, 1, 0, 0, 0, 0},
		"trailing bytes": {0, 0},
	}
	for name, payload := range cases {
		msg := Message{t: TypeTouchscreenReport, d: payload}
		if _, err := msg.TouchscreenReport(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
