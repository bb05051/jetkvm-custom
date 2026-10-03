package edidfit

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// The hand-made presets in edid_presets.go were generated with the same CVT
// formula; their preferred-mode descriptors must come out identical.
func TestCVTStandardMatchesPresets(t *testing.T) {
	cases := []struct {
		w, h int
		dtd  string
	}{
		{1920, 800, "D430805072201F3060C83A00B45A0000001C"},
		{1440, 1088, "AF32A0E05140294058983A00B45A0000001C"},
		{1600, 1000, "A933401062E8263060A83600B45A0000001C"},
	}
	for _, c := range cases {
		want, _ := hex.DecodeString(c.dtd)
		got := bytes.Clone(want)
		WriteDTD(got, CVTStandard(c.w, c.h, RefreshHz))
		if !bytes.Equal(got, want) {
			t.Errorf("%dx%d: DTD %X, want %X", c.w, c.h, got, want)
		}
	}
}

func TestFit(t *testing.T) {
	areas := [][2]int{{1666, 689}, {1920, 1080}, {390, 844}, {1080, 1080}, {3000, 400}, {400, 3000}, {1, 1}}
	for _, a := range areas {
		tm, err := Fit(a[0], a[1])
		if err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		w, h := tm.Width(), tm.Height()
		if w%Align != 0 || h%Align != 0 || w > MaxWidth || h > MaxHeight || w < MinWidth || h < MinHeight {
			t.Errorf("%v -> %dx%d out of bounds", a, w, h)
		}
		if tm.PixelClockKHz() > MaxPixelClockKHz || tm.HFreqKHz() > MaxHFreqKHz || tm.HFreqKHz() < MinHFreqKHz {
			t.Errorf("%v -> %dx%d: %d kHz pixel clock, %.1f kHz line rate", a, w, h, tm.PixelClockKHz(), tm.HFreqKHz())
		}
	}

	// A typical browser area keeps its aspect ratio within 2%.
	tm, _ := Fit(1666, 689)
	got, want := float64(tm.Width())/float64(tm.Height()), 1666.0/689.0
	if got/want < 0.98 || got/want > 1.02 {
		t.Errorf("1666x689 -> %dx%d, ratio %.3f want %.3f", tm.Width(), tm.Height(), got, want)
	}

	if _, err := Fit(0, 100); err == nil {
		t.Error("expected an error for an empty area")
	}
}

func TestBuild(t *testing.T) {
	template, _ := hex.DecodeString("00FFFFFFFFFFFF004C2D00000000000020130103803018780AB811A6554B9B25135054BFEF80714F8100814081809500950F01010101DB3300507280223068C03A00B45A0000001C662156AA51001E30468F3300A05A0000001E000000FD00384B1E510E010A202020202020000000FC0053796E634D61737465720A2020018C020313F0230907078301000066030C001000809A29A0D05184223050983600905A0000001C000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000B7")
	tm, _ := Fit(1666, 689)
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
