import { useCallback, useEffect, useRef, useState } from "react";
import { LuCheck, LuMonitor, LuSettings, LuSquare, LuSquareCheck } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useSettingsStore, useVideoStore } from "@hooks/stores";
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

// Auto fit: how often to look at the video area, and how far its aspect
// ratio may drift from the last fit before re-fitting. Aligning the mode to
// multiples of 16 alone can change the ratio by up to ~1.8%.
const AUTO_FIT_INTERVAL_MS = 10_000;
const AUTO_FIT_TOLERANCE = 0.03;

// Primary click: set an EDID matching the current video area once.
// Caret: auto fit toggle, then the EDID presets from the video settings.
export default function ResolutionButton() {
  const { send } = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();
  const { autoFitResolution, setAutoFitResolution } = useSettingsStore();

  const [presets, setPresets] = useState<EDIDPreset[]>([]);
  // Lowercased EDID currently set on the device
  const [currentEdid, setCurrentEdid] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  // Aspect ratio of the area the current fit was made for
  const lastFitRatio = useRef<number | null>(null);

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

  const fitToWindow = useCallback(() => {
    const { clientWidth, clientHeight } = useVideoStore.getState();
    const width = Math.round(clientWidth);
    const height = Math.round(clientHeight);
    if (!width || !height) {
      notifications.error(m.resolution_fit_failed({ error: m.resolution_fit_no_area() }));
      return;
    }
    setBusy(true);
    busyRef.current = true;
    void send("setFitEDID", { width, height }, (resp: JsonRpcResponse) => {
      setBusy(false);
      busyRef.current = false;
      if ("error" in resp) {
        notifications.error(
          m.resolution_fit_failed({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      lastFitRatio.current = width / height;
      const result = resp.result as { width: number; height: number };
      notifications.success(m.resolution_fit_success(result));
      refresh();
    });
  }, [send, refresh]);

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
  const edidKnown = currentEdid !== null;

  // After a page load the device may already carry a fitted EDID: use its
  // mode as the reference so opening the console does not re-fit.
  useEffect(() => {
    if (lastFitRatio.current === null && fitted) {
      lastFitRatio.current = fitted.width / fitted.height;
    }
  }, [fitted]);

  useEffect(() => {
    if (!autoFitResolution || !edidKnown) return;
    const check = () => {
      if (document.hidden || busyRef.current) return;
      const { clientWidth, clientHeight } = useVideoStore.getState();
      if (!clientWidth || !clientHeight) return;
      const last = lastFitRatio.current;
      if (last && Math.abs(clientWidth / clientHeight / last - 1) < AUTO_FIT_TOLERANCE) return;
      fitToWindow();
    };
    check();
    const id = window.setInterval(check, AUTO_FIT_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [autoFitResolution, edidKnown, fitToWindow]);

  const menuItems: SplitButtonMenuItem[] = [
    {
      label: m.resolution_auto_fit(),
      icon: autoFitResolution ? LuSquareCheck : LuSquare,
      active: autoFitResolution,
      onClick: () => setAutoFitResolution(!autoFitResolution),
    },
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
