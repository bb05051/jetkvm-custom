package usbgadget

import (
	"testing"
)

func checkCollectionsBalanced(t *testing.T, desc []byte) {
	t.Helper()
	depth := 0
	for i := 0; i < len(desc); {
		prefix := desc[i]
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		switch prefix & 0xFC {
		case 0xA0: // Collection
			depth++
		case 0xC0: // End Collection
			depth--
		}
		if depth < 0 {
			t.Fatalf("unbalanced End Collection at byte %d", i)
		}
		i += 1 + size
		if i > len(desc) {
			t.Fatalf("item at byte %d overruns descriptor", i)
		}
	}
	if depth != 0 {
		t.Fatalf("unclosed collections: %d", depth)
	}
}

func TestTouchscreenReportDescCollections(t *testing.T) {
	checkCollectionsBalanced(t, touchscreenReportDesc)
}

// Linux's hid-core switches a multitouch device to HID_GROUP_MULTITOUCH_WIN_8
// when a Feature item carries usage 0xff0000c5 with Report Size 8 and Report
// Count 256.
func TestTouchscreenDescHasWin8CertificationFeature(t *testing.T) {
	var usagePage, reportSize, reportCount uint32
	var usages []uint32
	found := false

	desc := touchscreenReportDesc
	for i := 0; i < len(desc); {
		prefix := desc[i]
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		var val uint32
		for j := 0; j < size; j++ {
			val |= uint32(desc[i+1+j]) << (8 * j)
		}
		switch prefix & 0xFC {
		case 0x04: // Usage Page
			usagePage = val
		case 0x74: // Report Size
			reportSize = val
		case 0x94: // Report Count
			reportCount = val
		case 0x08: // Usage
			usages = append(usages, usagePage<<16|val)
		case 0x80, 0x90, 0xA0: // Input, Output, Collection
			usages = nil
		case 0xB0: // Feature
			for _, u := range usages {
				if u == 0xff0000c5 && reportSize == 8 && reportCount == 256 {
					found = true
				}
			}
			usages = nil
		}
		i += 1 + size
	}

	if !found {
		t.Fatal("missing Device Certification Status feature (0xff0000c5, 8x256)")
	}
}
