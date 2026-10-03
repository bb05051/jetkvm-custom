// Package edidfit builds EDIDs whose preferred mode matches a requested
// aspect ratio ("fit to window"). It has no device dependencies so it can be
// tested on the build host.
package edidfit

import (
	"fmt"
	"math"
)

// "Fit to window": build an EDID whose preferred mode matches the aspect
// ratio of the browser's video area, so the host fills it without bars.
//
// The TC358743 drops lines with reduced-blanking timings, so the mode uses
// CVT standard blanking and stays within what the bridge and the template's
// range limits accept.
const (
	TargetPixels     = 1600 * 900 // only the aspect ratio follows the window
	MaxWidth         = 1920
	MaxHeight        = 1200
	MinWidth         = 640
	MinHeight        = 512    // 480 lines would put the line rate just under 30 kHz
	MaxPixelClockKHz = 140000 // template range limits: 140 MHz, 30-81 kHz
	MinHFreqKHz      = 30
	MaxHFreqKHz      = 81
	RefreshHz        = 60
	Align            = 16
	ProductCode      = 0x0010 // distinguishes fitted EDIDs from the presets
)

type Timing struct {
	hActive, hBlank, hFront, hSync int
	vActive, vBlank, vFront, vSync int
	pixelClockKHz                  int
}

// Width and Height return the active resolution.
func (t Timing) Width() int  { return t.hActive }
func (t Timing) Height() int { return t.vActive }

// PixelClockKHz returns the pixel clock.
func (t Timing) PixelClockKHz() int { return t.pixelClockKHz }

func (t Timing) hTotal() int { return t.hActive + t.hBlank }

// HFreqKHz returns the horizontal line rate.
func (t Timing) HFreqKHz() float64 {
	return float64(t.pixelClockKHz) / float64(t.hTotal())
}

// cvtVSync returns the CVT vertical sync width for the aspect ratio.
func cvtVSync(w, h int) int {
	switch {
	case w*3 == h*4:
		return 4
	case w*9 == h*16:
		return 5
	case w*10 == h*16:
		return 6
	case w*4 == h*5, w*9 == h*15:
		return 7
	default:
		return 10
	}
}

// CVTStandard implements VESA CVT 1.1 with standard (not reduced) blanking.
func CVTStandard(w, h, refresh int) Timing {
	const (
		cPrime, mPrime  = 30.0, 300.0 // blanking formula offset and gradient
		minVSyncBPUs    = 550.0
		minVPorch       = 3
		minVBackPorch   = 6
		cellGranularity = 8
		hSyncPercent    = 8.0
		clockStepKHz    = 250
	)
	vSync := cvtVSync(w, h)
	hPeriodUs := (1e6/float64(refresh) - minVSyncBPUs) / float64(h+minVPorch)
	vSyncBP := int(math.Floor(minVSyncBPUs/hPeriodUs)) + 1
	if vSyncBP < vSync+minVBackPorch {
		vSyncBP = vSync + minVBackPorch
	}
	duty := cPrime - mPrime*hPeriodUs/1000
	hBlank := int(math.Floor(float64(w)*duty/(100-duty)/(2*cellGranularity))) * 2 * cellGranularity
	hTotal := w + hBlank
	pixelClockKHz := int(math.Floor(float64(hTotal)/hPeriodUs*1000/clockStepKHz)) * clockStepKHz
	hSync := int(math.Floor(hSyncPercent/100*float64(hTotal)/cellGranularity)) * cellGranularity
	hBackPorch := hBlank / 2
	return Timing{
		hActive: w, hBlank: hBlank, hFront: hBlank - hSync - hBackPorch, hSync: hSync,
		vActive: h, vBlank: vSyncBP + minVPorch, vFront: minVPorch, vSync: vSync,
		pixelClockKHz: pixelClockKHz,
	}
}

// alignNearest rounds v to the nearest multiple of Align within [lo, hi].
func alignNearest(v float64, lo, hi int) int {
	a := int(math.Round(v/Align)) * Align
	return max(lo, min(a, hi/Align*Align))
}

// Fit picks a mode with the aspect ratio of areaW:areaH.
func Fit(areaW, areaH int) (Timing, error) {
	if areaW <= 0 || areaH <= 0 {
		return Timing{}, fmt.Errorf("invalid video area %dx%d", areaW, areaH)
	}
	ratio := float64(areaW) / float64(areaH)
	minRatio := float64(MinWidth) / MaxHeight
	maxRatio := float64(MaxWidth) / MinHeight
	ratio = math.Max(minRatio, math.Min(maxRatio, ratio))

	h := math.Sqrt(TargetPixels / ratio)
	w := h * ratio
	if w > MaxWidth {
		w, h = MaxWidth, MaxWidth/ratio
	}
	if h > MaxHeight {
		w, h = MaxHeight*ratio, MaxHeight
	}

	// Shrink until the timing fits the pixel clock and line rate limits.
	for scale := 1.0; scale > 0.3; scale -= 0.02 {
		tw := alignNearest(w*scale, MinWidth, MaxWidth)
		th := alignNearest(h*scale, MinHeight, MaxHeight)
		t := CVTStandard(tw, th, RefreshHz)
		if t.pixelClockKHz <= MaxPixelClockKHz && t.HFreqKHz() <= MaxHFreqKHz && t.HFreqKHz() >= MinHFreqKHz {
			return t, nil
		}
	}
	return Timing{}, fmt.Errorf("no mode fits %dx%d", areaW, areaH)
}

// WriteDTD encodes t as an 18-byte detailed timing descriptor, keeping the
// image size and sync flags of the template descriptor.
func WriteDTD(dtd []byte, t Timing) {
	pc := t.pixelClockKHz / 10
	dtd[0], dtd[1] = byte(pc), byte(pc>>8)
	dtd[2], dtd[3] = byte(t.hActive), byte(t.hBlank)
	dtd[4] = byte((t.hActive>>8)<<4 | (t.hBlank >> 8))
	dtd[5], dtd[6] = byte(t.vActive), byte(t.vBlank)
	dtd[7] = byte((t.vActive>>8)<<4 | (t.vBlank >> 8))
	dtd[8], dtd[9] = byte(t.hFront), byte(t.hSync)
	dtd[10] = byte((t.vFront&0xF)<<4 | (t.vSync & 0xF))
	dtd[11] = byte((t.hFront>>8)<<6 | (t.hSync>>8)<<4 | (t.vFront>>4)<<2 | (t.vSync >> 4))
}

func checksum(block []byte) byte {
	var sum byte
	for _, b := range block[:127] {
		sum += b
	}
	return -sum
}

// Build returns template (a 128-byte base block, optionally followed by
// extensions) with t as the preferred mode, the fit product code and a
// per-mode serial number, so hosts treat each fit as a new monitor and use
// its preferred mode instead of a remembered one.
func Build(template []byte, t Timing) ([]byte, error) {
	if len(template) < 128 || len(template)%128 != 0 {
		return nil, fmt.Errorf("invalid EDID template length %d", len(template))
	}
	edid := append([]byte(nil), template...)
	edid[10], edid[11] = byte(ProductCode), byte(ProductCode>>8)
	serial := uint32(t.hActive)<<16 | uint32(t.vActive)
	edid[12], edid[13], edid[14], edid[15] = byte(serial), byte(serial>>8), byte(serial>>16), byte(serial>>24)
	WriteDTD(edid[54:72], t)
	edid[127] = checksum(edid[:128])
	return edid, nil
}
