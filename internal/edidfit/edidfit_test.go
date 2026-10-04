package edidfit

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Regression vectors: descriptors Mode produced for modes verified on the
// device (formerly the custom presets); they must come out identical.
func TestModeMatchesPresets(t *testing.T) {
	cases := []struct {
		w, h int
		dtd  string
	}{
		{1920, 800, "6536806873201F3060C83A00B45A0000001C"},
		{1440, 1088, "5834A0185240294058983A00B45A0000001C"},
		{1600, 1000, "3935404862E8263060A83600B45A0000001C"},
	}
	for _, c := range cases {
		want, _ := hex.DecodeString(c.dtd)
		got := bytes.Clone(want)
		WriteDTD(got, Mode(c.w, c.h))
		if !bytes.Equal(got, want) {
			t.Errorf("%dx%d: DTD %X, want %X", c.w, c.h, got, want)
		}
	}
}

func TestFit(t *testing.T) {
	areas := [][2]int{{1666, 689}, {1920, 1080}, {390, 844}, {1080, 1080}, {3000, 400}, {400, 3000}, {1, 1}}
	for _, a := range areas {
		tm, err := Fit(a[0], a[1], Bases[DefaultBase])
		if err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		w, h := tm.Width(), tm.Height()
		if w%Align != 0 || h%AlignHeight != 0 || w > MaxWidth || h > MaxHeight || w < MinWidth || h < MinHeight {
			t.Errorf("%v -> %dx%d out of bounds", a, w, h)
		}
		if tm.PixelClockKHz() > MaxPixelClockKHz || tm.HFreqKHz() > MaxHFreqKHz || tm.HFreqKHz() < MinHFreqKHz {
			t.Errorf("%v -> %dx%d: %d kHz pixel clock, %.1f kHz line rate", a, w, h, tm.PixelClockKHz(), tm.HFreqKHz())
		}
	}

	// Every fitted mode must keep the bridge fed and fit the DTD fields.
	for _, a := range areas {
		tm, _ := Fit(a[0], a[1], Bases[DefaultBase])
		shortfall := float64(tm.hActive) * (1 - float64(tm.pixelClockKHz)/CSIPixelRateKHz)
		if shortfall > MaxCSIShortfall {
			t.Errorf("%v -> %dx%d: CSI shortfall %.0f px", a, tm.Width(), tm.Height(), shortfall)
		}
		if tm.hBlank > 4095 || tm.vBlank > 4095 || tm.hFront > 1023 || tm.hSync > 1023 || tm.vFront > 63 || tm.vSync > 63 {
			t.Errorf("%v -> %+v does not fit the DTD fields", a, tm)
		}
	}

	// A typical browser area keeps its aspect ratio within 2%.
	tm, _ := Fit(1666, 689, Bases[DefaultBase])
	got, want := float64(tm.Width())/float64(tm.Height()), 1666.0/689.0
	if got/want < 0.98 || got/want > 1.02 {
		t.Errorf("1666x689 -> %dx%d, ratio %.3f want %.3f", tm.Width(), tm.Height(), got, want)
	}

	if _, err := Fit(0, 100, Bases[DefaultBase]); err == nil {
		t.Error("expected an error for an empty area")
	}
}

func TestBuild(t *testing.T) {
	template, _ := hex.DecodeString("00FFFFFFFFFFFF004C2D00000000000020130103803018780AB811A6554B9B25135054BFEF80714F8100814081809500950F01010101DB3300507280223068C03A00B45A0000001C662156AA51001E30468F3300A05A0000001E000000FD00384B1E510E010A202020202020000000FC0053796E634D61737465720A2020018C020313F0230907078301000066030C001000809A29A0D05184223050983600905A0000001C000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000B7")
	tm, _ := Fit(1666, 689, Bases[DefaultBase])
	edid, err := Build(template, tm)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(edid); i += 128 {
		var sum byte
		for _, v := range edid[i : i+128] {
			sum += v
		}
		if sum != 0 {
			t.Errorf("block %d checksum %d", i/128, sum)
		}
	}
	if edid[10] != byte(ProductCode) || edid[11] != byte(ProductCode>>8) {
		t.Errorf("product code %#x%02x", edid[11], edid[10])
	}
	if !bytes.Equal(edid[128:], template[128:]) {
		t.Error("extension blocks changed")
	}
	if bytes.Equal(edid[54:72], template[54:72]) {
		t.Error("preferred mode not replaced")
	}
}

// Every base works for every area: within limits, bridge safe, and the
// bigger the base the bigger the mode. 1920x1080 itself needs reduced blanking.
func TestFitBases(t *testing.T) {
	for _, a := range [][2]int{{1920, 1080}, {1666, 689}, {2448, 1848}, {393, 852}} {
		prev := 0
		for _, base := range []string{"1280x720", "1600x900", "1920x1080"} {
			tm, err := Fit(a[0], a[1], Bases[base])
			if err != nil {
				t.Fatalf("%v %s: %v", a, base, err)
			}
			shortfall := float64(tm.hActive) * (1 - float64(tm.pixelClockKHz)/CSIPixelRateKHz)
			if !withinLimits(tm) || shortfall > MaxCSIShortfall {
				t.Errorf("%v %s -> %dx%d @ %d kHz, shortfall %.0f", a, base, tm.Width(), tm.Height(), tm.pixelClockKHz, shortfall)
			}
			if px := tm.Width() * tm.Height(); px < prev {
				t.Errorf("%v %s -> %dx%d is smaller than the previous base", a, base, tm.Width(), tm.Height())
			} else {
				prev = px
			}
		}
	}
	if tm, _ := Fit(1920, 1080, Bases["1920x1080"]); tm.Width() != 1920 || tm.Height() != 1080 {
		t.Errorf("16:9 at the 1920x1080 base -> %dx%d, want 1920x1080", tm.Width(), tm.Height())
	}
}
