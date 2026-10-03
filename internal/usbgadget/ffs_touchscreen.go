package usbgadget

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// The touchscreen HID interface is served from userspace through FunctionFS
// so that control requests can be answered properly. In particular Windows
// reads the Contact Count Maximum feature report with GET_REPORT and ignores
// a touchscreen that reports 0, which is all f_hid can answer.
//
// Lifecycle: the function directory and the functionfs mount are created
// before the gadget transaction, and the descriptors are written to ep0
// before the gadget is bound (binding fails otherwise). ep0 then stays open
// for the lifetime of the process. When the process exits, the kernel
// unbinds the gadget; the next process rebinds it during Init.

const (
	touchFFSInstance  = "touchscreen"
	touchFFSMountPath = "/run/jetkvm/ffs-touchscreen"

	ffsDescriptorsMagicV2 = 3
	ffsStringsMagic       = 2
	ffsHasFSDesc          = 1
	ffsHasHSDesc          = 2

	ffsEventBind    = 0
	ffsEventUnbind  = 1
	ffsEventEnable  = 2
	ffsEventDisable = 3
	ffsEventSetup   = 4
	ffsEventSuspend = 5
	ffsEventResume  = 6
	ffsEventSize    = 12 // struct usb_functionfs_event

	usbReqGetDescriptor = 0x06
	hidDescriptorType   = 0x21
	hidReportDescType   = 0x22

	hidReqGetReport   = 0x01
	hidReqGetIdle     = 0x02
	hidReqGetProtocol = 0x03
	hidReqSetReport   = 0x09
	hidReqSetIdle     = 0x0A
	hidReqSetProtocol = 0x0B

	hidReportTypeInput   = 1
	hidReportTypeFeature = 3

	touchEpMaxPacket = 64
)

type setupAction int

const (
	setupStall setupAction = iota
	setupReply             // IN: write data to ep0
	setupAck               // OUT: read wLength bytes from ep0
)

type ffsTouchscreen struct {
	log        *zerolog.Logger
	reportDesc []byte

	ep0 int
	ep1 int

	enabled   atomic.Bool
	suspended atomic.Bool

	reports chan []byte

	stateLock  sync.Mutex
	lastReport []byte
	idle       byte
	protocol   byte
}

// ensureTouchscreenFFS prepares the FunctionFS touchscreen so that the next
// gadget transaction can link and bind it. Called with configLock held.
func (u *UsbGadget) ensureTouchscreenFFS() error {
	if u.touchFFS.ready() {
		return nil
	}

	// At boot this runs before the gadget transaction mounts configfs.
	configfsMounted, err := isMountedAs(configFSPath, "configfs")
	if err != nil {
		return err
	}
	if !configfsMounted {
		if err := mountConfigFS(configFSPath); err != nil {
			return err
		}
	}

	funcPath := joinPath(u.kvmGadgetPath, touchscreenConfig.path)
	if err := os.MkdirAll(funcPath, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", funcPath, err)
	}

	if err := os.MkdirAll(touchFFSMountPath, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", touchFFSMountPath, err)
	}
	mounted, err := isMountedAs(touchFFSMountPath, "functionfs")
	if err != nil {
		return err
	}
	if !mounted {
		if err := unix.Mount(touchFFSInstance, touchFFSMountPath, "functionfs", 0, ""); err != nil {
			return fmt.Errorf("mount functionfs: %w", err)
		}
	}

	ft, err := startFFSTouchscreen(touchFFSMountPath, touchscreenReportDesc, u.log)
	if err != nil {
		return err
	}
	u.touchFFS = ft
	u.log.Info().Str("mount", touchFFSMountPath).Msg("touchscreen FunctionFS ready")
	return nil
}

func isMountedAs(dir string, fsType string) (bool, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && fields[1] == dir && fields[2] == fsType {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func startFFSTouchscreen(dir string, reportDesc []byte, log *zerolog.Logger) (*ffsTouchscreen, error) {
	ep0, err := unix.Open(path.Join(dir, "ep0"), unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open ep0: %w", err)
	}
	if _, err := unix.Write(ep0, ffsTouchscreenDescriptors(len(reportDesc))); err != nil {
		unix.Close(ep0)
		return nil, fmt.Errorf("write descriptors: %w", err)
	}
	if _, err := unix.Write(ep0, ffsEmptyStrings()); err != nil {
		unix.Close(ep0)
		return nil, fmt.Errorf("write strings: %w", err)
	}
	// A blocking ep0 read sleeps while holding the FunctionFS mutex, and
	// functionfs_unbind() needs that mutex: a rebind would then hang the
	// gadget (and this process) until an event arrives. Wait in poll()
	// instead, which only takes the mutex briefly, and read non-blocking.
	if err := unix.SetNonblock(ep0, true); err != nil {
		unix.Close(ep0)
		return nil, fmt.Errorf("set ep0 non-blocking: %w", err)
	}

	// Non-blocking so a write fails fast instead of waiting for the host to
	// enable the endpoint; once enabled, writes complete on the next poll.
	ep1, err := unix.Open(path.Join(dir, "ep1"), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(ep0)
		return nil, fmt.Errorf("open ep1: %w", err)
	}

	ft := &ffsTouchscreen{
		log:        log,
		reportDesc: reportDesc,
		ep0:        ep0,
		ep1:        ep1,
		reports:    make(chan []byte, 64),
		protocol:   1, // report protocol
	}
	go ft.ep0Loop()
	go ft.writeLoop()
	return ft, nil
}

func (ft *ffsTouchscreen) ready() bool {
	return ft != nil
}

func (ft *ffsTouchscreen) canSend() bool {
	return ft.enabled.Load() && !ft.suspended.Load()
}

// sendReport queues an input report, dropping the oldest one when the host
// is not keeping up. Reports are dropped while the host has not enabled the
// interface, so stale contacts are never replayed after enumeration.
func (ft *ffsTouchscreen) sendReport(report []byte) {
	ft.stateLock.Lock()
	ft.lastReport = report
	ft.stateLock.Unlock()

	if !ft.canSend() {
		return
	}
	select {
	case ft.reports <- report:
	default:
		select {
		case <-ft.reports:
		default:
		}
		select {
		case ft.reports <- report:
		default:
		}
	}
}

func (ft *ffsTouchscreen) writeLoop() {
	for report := range ft.reports {
		if !ft.canSend() {
			continue
		}
		if _, err := unix.Write(ft.ep1, report); err != nil &&
			!errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.ESHUTDOWN) && !errors.Is(err, unix.EINTR) {
			ft.log.Debug().Err(err).Msg("touchscreen: failed to write input report")
		}
	}
}

func (ft *ffsTouchscreen) ep0Loop() {
	buf := make([]byte, ffsEventSize*4)
	fds := []unix.PollFd{{Fd: int32(ft.ep0), Events: unix.POLLIN}}
	for {
		fds[0].Revents = 0
		if _, err := unix.Poll(fds, 1000); err != nil && !errors.Is(err, unix.EINTR) {
			ft.log.Error().Err(err).Msg("touchscreen: ep0 poll failed, stopping")
			ft.enabled.Store(false)
			return
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}

		n, err := unix.Read(ft.ep0, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			ft.log.Error().Err(err).Msg("touchscreen: ep0 read failed, stopping")
			ft.enabled.Store(false)
			return
		}

		for off := 0; off+ffsEventSize <= n; off += ffsEventSize {
			event := buf[off : off+ffsEventSize]
			switch event[8] {
			case ffsEventEnable:
				ft.suspended.Store(false)
				ft.enabled.Store(true)
				ft.log.Debug().Msg("touchscreen: enabled by host")
			case ffsEventDisable, ffsEventUnbind:
				ft.enabled.Store(false)
			case ffsEventSuspend:
				ft.suspended.Store(true)
			case ffsEventResume:
				ft.suspended.Store(false)
			case ffsEventSetup:
				ft.handleSetup([8]byte(event[:8]))
			case ffsEventBind:
			}
		}
	}
}

func (ft *ffsTouchscreen) handleSetup(setup [8]byte) {
	isIn := setup[0]&0x80 != 0
	wLength := binary.LittleEndian.Uint16(setup[6:8])

	action, data := ft.setupResponse(setup)

	var err error
	switch action {
	case setupReply:
		if len(data) > int(wLength) {
			data = data[:wLength]
		}
		err = retryEAGAIN(func() error { _, err := unix.Write(ft.ep0, data); return err })
	case setupAck:
		ack := make([]byte, wLength)
		err = retryEAGAIN(func() error { _, err := unix.Read(ft.ep0, ack); return err })
	case setupStall:
		// Doing the opposite of the data direction stalls the request.
		if isIn {
			_ = retryEAGAIN(func() error { _, err := unix.Read(ft.ep0, nil); return err })
		} else {
			_ = retryEAGAIN(func() error { _, err := unix.Write(ft.ep0, nil); return err })
		}
		ft.log.Trace().Hex("setup", setup[:]).Msg("touchscreen: stalled control request")
		return
	}
	if err != nil {
		ft.log.Debug().Err(err).Hex("setup", setup[:]).Msg("touchscreen: control request failed")
	}
}

// setupResponse decides how to answer a control request addressed to the
// touchscreen interface.
func (ft *ffsTouchscreen) setupResponse(setup [8]byte) (setupAction, []byte) {
	bmRequestType := setup[0]
	bRequest := setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])

	ft.stateLock.Lock()
	defer ft.stateLock.Unlock()

	switch bmRequestType {
	case 0x81: // standard, device-to-host, interface
		if bRequest != usbReqGetDescriptor {
			return setupStall, nil
		}
		switch wValue >> 8 {
		case hidReportDescType:
			return setupReply, ft.reportDesc
		case hidDescriptorType:
			return setupReply, hidClassDescriptor(len(ft.reportDesc))
		}

	case 0xA1: // class, device-to-host, interface
		switch bRequest {
		case hidReqGetReport:
			reportType, reportID := byte(wValue>>8), byte(wValue)
			switch {
			case reportType == hidReportTypeFeature && reportID == touchscreenFeatureReportID:
				return setupReply, []byte{reportID, TouchscreenMaxContacts}
			case reportType == hidReportTypeFeature && reportID == touchscreenCertReportID:
				return setupReply, append([]byte{reportID}, make([]byte, 256)...)
			case reportType == hidReportTypeInput && reportID == touchscreenReportID:
				if ft.lastReport != nil {
					return setupReply, ft.lastReport
				}
				empty := make([]byte, touchscreenReportLength)
				empty[0] = touchscreenReportID
				return setupReply, empty
			}
		case hidReqGetIdle:
			return setupReply, []byte{ft.idle}
		case hidReqGetProtocol:
			return setupReply, []byte{ft.protocol}
		}

	case 0x21: // class, host-to-device, interface
		switch bRequest {
		case hidReqSetIdle:
			ft.idle = byte(wValue >> 8)
			return setupAck, nil
		case hidReqSetProtocol:
			ft.protocol = byte(wValue)
			return setupAck, nil
		case hidReqSetReport:
			return setupAck, nil
		}
	}

	return setupStall, nil
}

// retryEAGAIN retries an ep0 operation that failed only because the
// FunctionFS mutex was momentarily held (non-blocking ep0 uses a trylock).
func retryEAGAIN(op func() error) error {
	var err error
	for range 20 {
		if err = op(); !errors.Is(err, unix.EAGAIN) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	return err
}

func hidClassDescriptor(reportDescLen int) []byte {
	return []byte{
		9,                 // bLength
		hidDescriptorType, // bDescriptorType (HID)
		0x11, 0x01,        // bcdHID 1.11
		0,                 // bCountryCode
		1,                 // bNumDescriptors
		hidReportDescType, // bDescriptorType (Report)
		byte(reportDescLen), byte(reportDescLen >> 8),
	}
}

// ffsTouchscreenDescriptors builds the FunctionFS v2 descriptor blob: one HID
// interface with a single interrupt IN endpoint, for full and high speed.
func ffsTouchscreenDescriptors(reportDescLen int) []byte {
	intf := []byte{
		9, 4, // bLength, bDescriptorType (Interface)
		0, 0, // bInterfaceNumber (remapped by the kernel), bAlternateSetting
		1,       // bNumEndpoints
		3, 0, 0, // bInterfaceClass (HID), no subclass, no protocol
		0, // iInterface
	}
	hid := hidClassDescriptor(reportDescLen)
	endpoint := func(interval byte) []byte {
		return []byte{
			7, 5, // bLength, bDescriptorType (Endpoint)
			0x81,                                           // bEndpointAddress (IN 1)
			3,                                              // bmAttributes (Interrupt)
			touchEpMaxPacket & 0xFF, touchEpMaxPacket >> 8, // wMaxPacketSize
			interval,
		}
	}

	var fs, hs []byte
	fs = append(append(append(fs, intf...), hid...), endpoint(1)...) // 1 ms
	hs = append(append(append(hs, intf...), hid...), endpoint(4)...) // 2^(4-1) * 125 us = 1 ms

	const header = 5 * 4
	blob := make([]byte, header, header+len(fs)+len(hs))
	binary.LittleEndian.PutUint32(blob[0:], ffsDescriptorsMagicV2)
	binary.LittleEndian.PutUint32(blob[4:], uint32(header+len(fs)+len(hs)))
	binary.LittleEndian.PutUint32(blob[8:], ffsHasFSDesc|ffsHasHSDesc)
	binary.LittleEndian.PutUint32(blob[12:], 3) // fs descriptor count
	binary.LittleEndian.PutUint32(blob[16:], 3) // hs descriptor count
	return append(append(blob, fs...), hs...)
}

func ffsEmptyStrings() []byte {
	blob := make([]byte, 16)
	binary.LittleEndian.PutUint32(blob[0:], ffsStringsMagic)
	binary.LittleEndian.PutUint32(blob[4:], 16)
	// str_count = 0, lang_count = 0
	return blob
}
