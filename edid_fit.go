package kvm

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/edidfit"
)

// fitTemplatePreset is the EDID the fitted modes are based on.
const fitTemplatePreset = "Samsung SyncMaster, 1792x896"

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

	var template []byte
	for _, p := range edidPresets {
		if p.Name == fitTemplatePreset {
			template, err = hex.DecodeString(p.EDID)
			if err != nil {
				return fitEDIDResult{}, err
			}
		}
	}
	if template == nil {
		return fitEDIDResult{}, fmt.Errorf("fit EDID template %q not found", fitTemplatePreset)
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
