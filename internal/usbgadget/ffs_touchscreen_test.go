package usbgadget

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFFSTouchscreenDescriptors(t *testing.T) {
	blob := ffsTouchscreenDescriptors(len(touchscreenReportDesc))

	if got := binary.LittleEndian.Uint32(blob[0:]); got != ffsDescriptorsMagicV2 {
		t.Fatalf("magic = %d", got)
	}
	if got := binary.LittleEndian.Uint32(blob[4:]); int(got) != len(blob) {
		t.Fatalf("length = %d, blob is %d bytes", got, len(blob))
	}
	if got := binary.LittleEndian.Uint32(blob[8:]); got != ffsHasFSDesc|ffsHasHSDesc {
		t.Fatalf("flags = %#x", got)
	}

	// Walk the descriptors: both speeds must hold interface, HID, endpoint.
	want := []byte{4, hidDescriptorType, 5, 4, hidDescriptorType, 5}
	var types []byte
	for i := 20; i < len(blob); {
		l := int(blob[i])
		if l == 0 || i+l > len(blob) {
			t.Fatalf("bad descriptor length %d at %d", l, i)
		}
		types = append(types, blob[i+1])
		i += l
	}
	if !bytes.Equal(types, want) {
		t.Fatalf("descriptor types = %v, want %v", types, want)
	}

	hid := hidClassDescriptor(len(touchscreenReportDesc))
	if got := int(binary.LittleEndian.Uint16(hid[7:9])); got != len(touchscreenReportDesc) {
		t.Fatalf("HID descriptor report length = %d, want %d", got, len(touchscreenReportDesc))
	}
}

func setupPacket(bmRequestType, bRequest byte, wValue, wLength uint16) [8]byte {
	var s [8]byte
	s[0], s[1] = bmRequestType, bRequest
	binary.LittleEndian.PutUint16(s[2:], wValue)
	binary.LittleEndian.PutUint16(s[6:], wLength)
	return s
}

func TestFFSTouchscreenSetupResponse(t *testing.T) {
	ft := &ffsTouchscreen{reportDesc: touchscreenReportDesc, protocol: 1}

	cases := []struct {
		name   string
		setup  [8]byte
		action setupAction
		data   []byte
	}{
		{"report descriptor", setupPacket(0x81, usbReqGetDescriptor, 0x2200, 512), setupReply, touchscreenReportDesc},
		{"HID descriptor", setupPacket(0x81, usbReqGetDescriptor, 0x2100, 9), setupReply, hidClassDescriptor(len(touchscreenReportDesc))},
		{"contact count maximum", setupPacket(0xA1, hidReqGetReport, 0x0300|touchscreenFeatureReportID, 2), setupReply, []byte{touchscreenFeatureReportID, TouchscreenMaxContacts}},
		{"certification blob", setupPacket(0xA1, hidReqGetReport, 0x0300|touchscreenCertReportID, 257), setupReply, append([]byte{touchscreenCertReportID}, make([]byte, 256)...)},
		{"unknown feature", setupPacket(0xA1, hidReqGetReport, 0x0309, 2), setupStall, nil},
		{"set idle", setupPacket(0x21, hidReqSetIdle, 0x0000, 0), setupAck, nil},
		{"set report", setupPacket(0x21, hidReqSetReport, 0x0304, 2), setupAck, nil},
		{"get protocol", setupPacket(0xA1, hidReqGetProtocol, 0, 1), setupReply, []byte{1}},
		{"vendor request", setupPacket(0xC1, 0x01, 0, 4), setupStall, nil},
	}
	for _, c := range cases {
		action, data := ft.setupResponse(c.setup)
		if action != c.action || !bytes.Equal(data, c.data) {
			t.Errorf("%s: got (%d, %x), want (%d, %x)", c.name, action, data, c.action, c.data)
		}
	}

	// An input GET_REPORT before any touch returns an empty report with the ID.
	action, data := ft.setupResponse(setupPacket(0xA1, hidReqGetReport, 0x0100|touchscreenReportID, 64))
	if action != setupReply || len(data) != touchscreenReportLength || data[0] != touchscreenReportID {
		t.Errorf("input report: got (%d, %x)", action, data)
	}
}
