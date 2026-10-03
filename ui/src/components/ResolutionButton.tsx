import { useCallback, useEffect, useState } from "react";
import { LuCheck, LuMonitor, LuSettings } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useVideoStore } from "@hooks/stores";
import {
  SplitButtonCaret,
  SplitButtonGroup,
  SplitButtonMenuItem,
  SplitButtonPrimary,
} from "@components/SplitButton";
import notifications from "@/notifications";

interface EDIDPreset {
  name: string;
  edid: string;
}

// Product code of the EDIDs built by setFitEDID (internal/edidfit); their
// serial number holds width << 16 | height.
const FIT_PRODUCT_CODE_HEX = "1000"; // 0x0010, little endian

function fittedSize(edid: string): { width: number; height: number } | null {
  if (edid.length < 32 || edid.slice(20, 24).toLowerCase() !== FIT_PRODUCT_CODE_HEX) return null;
  const bytes = [24, 26, 28, 30].map(i => parseInt(edid.slice(i, i + 2), 16));
  return { width: bytes[2] | (bytes[3] << 8), height: bytes[0] | (bytes[1] << 8) };
}

// Primary click: set an EDID matching the current video area once.
// Caret: pick one of the EDID presets from the video settings.
export default function ResolutionButton() {
  const { send } = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();
  const { clientWidth, clientHeight } = useVideoStore();

  const [presets, setPresets] = useState<EDIDPreset[]>([]);
  // Lowercased EDID currently set on the device
  const [currentEdid, setCurrentEdid] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(() => {
    void send("getEDIDPresets", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setPresets(resp.result as EDIDPreset[]);
    });
    void send("getEDID", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setCurrentEdid((resp.result as string).toLowerCase());
    });
  }, [send]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const fitToWindow = () => {
    const width = Math.round(clientWidth);
    const height = Math.round(clientHeight);
    if (!width || !height) {
      notifications.error(m.resolution_fit_failed({ error: m.resolution_fit_no_area() }));
      return;
    }
    setBusy(true);
    void send("setFitEDID", { width, height }, (resp: JsonRpcResponse) => {
      setBusy(false);
      if ("error" in resp) {
        notifications.error(
          m.resolution_fit_failed({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      const result = resp.result as { width: number; height: number };
      notifications.success(m.resolution_fit_success(result));
      refresh();
    });
  };

  const selectPreset = (preset: EDIDPreset) => {
    setBusy(true);
    void send("setEDID", { edid: preset.edid }, (resp: JsonRpcResponse) => {
      setBusy(false);
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      notifications.success(m.video_edid_set_success({ edid: preset.name }));
      refresh();
    });
  };

  const fitted = currentEdid ? fittedSize(currentEdid) : null;
  const isPreset = presets.some(p => p.edid.toLowerCase() === currentEdid);

  const menuItems: SplitButtonMenuItem[] = [
    ...presets.map(preset => {
      const active = preset.edid.toLowerCase() === currentEdid;
      return {
        label: preset.name,
        icon: active ? LuCheck : undefined,
        active,
        disabled: busy,
        onClick: () => selectPreset(preset),
      };
    }),
    ...(currentEdid && !isPreset
      ? [
          {
            label: fitted ? m.resolution_fit_current(fitted) : m.video_edid_custom(),
            icon: LuCheck,
            active: true,
            disabled: true,
            onClick: () => undefined,
          },
        ]
      : []),
    {
      label: m.resolution_popover_more_settings(),
      icon: LuSettings,
      onClick: () => navigateTo("/settings/video"),
    },
  ];

  return (
    <SplitButtonGroup>
      <SplitButtonPrimary
        icon={LuMonitor}
        label={m.action_bar_resolution()}
        title={m.resolution_fit_tooltip()}
        disabled={busy}
        onClick={fitToWindow}
      />
      <SplitButtonCaret menuItems={menuItems} />
    </SplitButtonGroup>
  );
}
