package usbgadget

// The touchscreen is a FunctionFS function implemented in userspace (see
// ffs_touchscreen.go) rather than an f_hid function:
//   - f_hid only supports four HID functions (HIDG_MINORS), all in use.
//   - f_hid answers GET_REPORT with zeros, so Windows reads a Contact Count
//     Maximum of 0 and ignores the touchscreen.

var touchscreenConfig = gadgetConfigItem{
	order:      1004,
	device:     "ffs.touchscreen",
	path:       []string{"functions", "ffs.touchscreen"},
	configPath: []string{"ffs.touchscreen"},
}

// TouchscreenMaxContacts is the number of simultaneous contacts the
// touchscreen descriptor reports (enough for two-finger pinch and scroll).
const TouchscreenMaxContacts = 2

const (
	touchscreenReportID        = 3
	touchscreenFeatureReportID = 4
	touchscreenCertReportID    = 5

	// Report ID + per contact (tip/padding, id, x, y) + contact count
	touchscreenReportLength = 1 + TouchscreenMaxContacts*6 + 1
)

// touchscreenFingerDesc describes one contact slot: tip switch, contact
// identifier and absolute X/Y in the same 0-32767 range as the absolute mouse.
var touchscreenFingerDesc = []byte{
	0x09, 0x22, //   Usage (Finger)
	0xA1, 0x02, //   Collection (Logical)
	0x09, 0x42, //     Usage (Tip Switch)
	0x15, 0x00, //     Logical Minimum (0)
	0x25, 0x01, //     Logical Maximum (1)
	0x75, 0x01, //     Report Size (1)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x02, //     Input (Data, Var, Abs)
	0x75, 0x07, //     Report Size (7)
	0x81, 0x03, //     Input (Cnst, Var, Abs)
	0x09, 0x51, //     Usage (Contact Identifier)
	0x25, 0x7F, //     Logical Maximum (127)
	0x75, 0x08, //     Report Size (8)
	0x81, 0x02, //     Input (Data, Var, Abs)
	0x05, 0x01, //     Usage Page (Generic Desktop Ctrls)
	0x09, 0x30, //     Usage (X)
	0x09, 0x31, //     Usage (Y)
	0x26, 0xFF, 0x7F, //     Logical Maximum (32767)
	0x35, 0x00, //     Physical Minimum (0)
	0x46, 0xFF, 0x7F, //     Physical Maximum (32767)
	0x75, 0x10, //     Report Size (16)
	0x95, 0x02, //     Report Count (2)
	0x81, 0x02, //     Input (Data, Var, Abs)
	0x05, 0x0D, //     Usage Page (Digitizer)
	0xC0, //   End Collection
}

var touchscreenReportDesc = func() []byte {
	desc := []byte{
		0x05, 0x0D, // Usage Page (Digitizer)
		0x09, 0x04, // Usage (Touch Screen)
		0xA1, 0x01, // Collection (Application)

		// Report ID 3: contacts
		0x85, touchscreenReportID, //   Report ID (3)
	}
	for range TouchscreenMaxContacts {
		desc = append(desc, touchscreenFingerDesc...)
	}
	desc = append(desc,
		0x35, 0x00, //   Physical Minimum (0) = Reset Physical Minimum
		0x45, 0x00, //   Physical Maximum (0) = Reset Physical Maximum
		0x09, 0x54, //   Usage (Contact Count)
		0x15, 0x00, //   Logical Minimum (0)
		0x25, TouchscreenMaxContacts, //   Logical Maximum (2)
		0x75, 0x08, //   Report Size (8)
		0x95, 0x01, //   Report Count (1)
		0x81, 0x02, //   Input (Data, Var, Abs)

		// Report ID 4: Contact Count Maximum. f_hid answers GET_REPORT with
		// zeros, so hosts fall back to the Logical Maximum (Linux does this).
		0x85, touchscreenFeatureReportID, //   Report ID (4)
		0x09, 0x55, //   Usage (Contact Count Maximum)
		0x25, TouchscreenMaxContacts, //   Logical Maximum (2)
		0xB1, 0x02, //   Feature (Data, Var, Abs)

		// Report ID 5: Device Certification Status (Windows 8 THQA blob).
		// Linux classifies the device as a Win8 multitouch device when it
		// is present.
		0x06, 0x00, 0xFF, //   Usage Page (Vendor Defined 0xFF00)
		0x85, touchscreenCertReportID, //   Report ID (5)
		0x09, 0xC5, //   Usage (0xC5)
		0x15, 0x00, //   Logical Minimum (0)
		0x26, 0xFF, 0x00, //   Logical Maximum (255)
		0x75, 0x08, //   Report Size (8)
		0x96, 0x00, 0x01, //   Report Count (256)
		0xB1, 0x02, //   Feature (Data, Var, Abs)

		0xC0, // End Collection
	)
	return desc
}()

// TouchContact is a single contact in a touchscreen report.
type TouchContact struct {
	ID  uint8
	Tip bool
	X   uint16
	Y   uint16
}

func (u *UsbGadget) HasTouchscreen() bool {
	return u.enabledDevices.Touchscreen
}

// TouchscreenReport sends one frame of contacts. Contacts that were lifted
// since the previous frame must be included once with Tip set to false.
func (u *UsbGadget) TouchscreenReport(contacts []TouchContact) error {
	if !u.enabledDevices.Touchscreen || u.touchFFS == nil {
		return nil
	}

	if len(contacts) > TouchscreenMaxContacts {
		contacts = contacts[:TouchscreenMaxContacts]
	}

	report := make([]byte, 0, touchscreenReportLength)
	report = append(report, touchscreenReportID)
	for i := range TouchscreenMaxContacts {
		var c TouchContact
		if i < len(contacts) {
			c = contacts[i]
		}
		var tip byte
		if c.Tip {
			tip = 1
		}
		report = append(report,
			tip,
			c.ID&0x7F,
			byte(c.X), byte(c.X>>8),
			byte(c.Y), byte(c.Y>>8),
		)
	}
	report = append(report, byte(len(contacts)))

	u.touchFFS.sendReport(report)
	u.resetUserInputTime()
	return nil
}
