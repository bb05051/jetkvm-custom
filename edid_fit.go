package kvm

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/edidfit"
)

// fitTemplateEDID is the EDID the fitted modes are based on (a Samsung
// SyncMaster, 1792x896 with a CTA extension); the preferred mode, product code
// and serial are replaced per fit.
const fitTemplateEDID = "00FFFFFFFFFFFF004C2D00000000000020130103803018780AB811A6554B9B25" +
	"135054BFEF80714F8100814081809500950F01010101DB3300507280223068C0" +
	"3A00B45A0000001C662156AA51001E30468F3300A05A0000001E000000FD0038" +
	"4B1E510E010A202020202020000000FC0053796E634D61737465720A2020018C" +
	"020313F0230907078301000066030C001000809A29A0D0518422305098360090" +
	"5A0000001C000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000" +
	"00000000000000000000000000000000000000000000000000000000000000B7"

// edidChanges spaces out EDID changes by 10 s (each one is a hotplug for
// the host); requests in between are held and only the last one is applied.
var edidChanges = &edidfit.ChangeLimiter{
	Interval: 10 * time.Second,
	Apply:    applyEDID,
	Current:  func() string { return config.EdidString },
	OnError: func(err error) {
		logger.Warn().Err(err).Msg("failed to apply the held EDID change")
	},
}

// rpcSetEDID sets the EDID through the change limiter (presets, defaults).
func rpcSetEDID(edid string) error {
	_, err := edidChanges.Request(edid)
	return err
}

type fitEDIDResult struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// DelayMs > 0 means the change is held and applied after that time
	DelayMs int64 `json:"delayMs"`
}

// rpcSetFitEDID sets an EDID whose preferred mode matches the aspect ratio
// of the client's video area, at about the size of base ("1280x720",
// "1600x900" or "1920x1080"; empty means edidfit.DefaultBase).
func rpcSetFitEDID(width int, height int, base string) (fitEDIDResult, error) {
	if base == "" {
		base = edidfit.DefaultBase
	}
	targetPixels, ok := edidfit.Bases[base]
	if !ok {
		return fitEDIDResult{}, fmt.Errorf("unknown fit base %q", base)
	}

	t, err := edidfit.Fit(width, height, targetPixels)
	if err != nil {
		return fitEDIDResult{}, err
	}

	template, err := hex.DecodeString(fitTemplateEDID)
	if err != nil {
		return fitEDIDResult{}, fmt.Errorf("fit EDID template: %w", err)
	}

	edid, err := edidfit.Build(template, t)
	if err != nil {
		return fitEDIDResult{}, err
	}
	delay, err := edidChanges.Request(strings.ToUpper(hex.EncodeToString(edid)))
	if err != nil {
		return fitEDIDResult{}, err
	}
	return fitEDIDResult{Width: t.Width(), Height: t.Height(), DelayMs: delay.Milliseconds()}, nil
}
