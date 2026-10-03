package kvm

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jetkvm/kvm/internal/edidfit"
)

// fitTemplatePreset is the EDID the fitted modes are based on.
const fitTemplatePreset = "Samsung SyncMaster, 1792x896"

type fitEDIDResult struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// rpcSetFitEDID sets an EDID whose preferred mode matches the aspect ratio
// of the client's video area (see internal/edidfit).
func rpcSetFitEDID(width int, height int) (fitEDIDResult, error) {
	t, err := edidfit.Fit(width, height)
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
	encoded := strings.ToUpper(hex.EncodeToString(edid))
	// Same mode as already set: leave the host alone (no hotplug, no flicker).
	if strings.EqualFold(encoded, config.EdidString) {
		return fitEDIDResult{Width: t.Width(), Height: t.Height()}, nil
	}
	if err := rpcSetEDID(encoded); err != nil {
		return fitEDIDResult{}, err
	}
	return fitEDIDResult{Width: t.Width(), Height: t.Height()}, nil
}
