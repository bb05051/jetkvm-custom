package kvm

import "errors"

// customFirmware marks this build as the custom firmware (see custom/README.md).
// Official updates replace it, so automatic and remote (MQTT) updates are
// disabled; updates can still be started from the web UI, which warns first.
const customFirmware = true

var errAutoUpdateDisabled = errors.New("automatic updates are disabled in the custom firmware")

// disableAutoUpdateForCustomFirmware persists auto_update_enabled=false so
// every reader of the config (UI, OTA status) agrees that it is off.
func disableAutoUpdateForCustomFirmware() {
	if !customFirmware || !config.AutoUpdateEnabled {
		return
	}
	config.AutoUpdateEnabled = false
	if err := SaveConfig(); err != nil {
		logger.Warn().Err(err).Msg("failed to persist disabled auto-update")
		return
	}
	logger.Info().Msg("auto-update disabled for the custom firmware")
}
