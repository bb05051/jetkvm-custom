import { useCallback, useEffect, useRef, useState } from "react";
import { LuMonitor } from "react-icons/lu";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";
import { useLocation } from "react-router";
import { ChevronDownIcon } from "@heroicons/react/16/solid";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { FIT_BASES, FitBase, useSettingsStore, useUiStore, useVideoStore } from "@hooks/stores";
import { SplitButtonGroup, SplitButtonPrimary } from "@components/SplitButton";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { CheckboxWithLabel } from "@components/Checkbox";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { cx } from "@/cva.config";
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
// ratio may drift from the last fit before re-fitting. Aligning the mode
// alone can change the ratio by up to ~1.8%.
// The check only compares ratios in the browser: nothing is sent to the
// device unless the ratio moved. Fullscreen is checked the same way, since
// rotating the client changes its area too.
const AUTO_FIT_INTERVAL_MS = 10_000;
const AUTO_FIT_TOLERANCE = 0.03;
// Delay before the first check after auto fit turns on or fullscreen
// changes, so the layout has settled.
const AUTO_FIT_SETTLE_MS = 1_000;

// The device holds EDID changes made within 10 s of the previous one and
// applies the last of them afterwards; re-read the state once that happened.
const HELD_CHANGE_REFRESH_MARGIN_MS = 1_500;

// While another device has taken over the session, this tab must not change
// the resolution (the "other session" popup is shown over the console).
const isOtherSessionPath = (path: string) => /\/other-session\/?$/i.test(path);

// Matches the caret half of SplitButton (which only offers a Menu caret).
const caretClass = cx(
  "inline-flex h-[28px] cursor-pointer items-center rounded-r-sm px-1 select-none",
  "border border-slate-800/30 border-l-slate-800/15 bg-white text-black shadow-xs outline-hidden",
  "dark:border-slate-300/20 dark:border-l-slate-300/10 dark:bg-slate-800 dark:text-white",
  "transition-all duration-200 hover:bg-blue-50/80 active:bg-blue-100/60",
  "dark:hover:bg-slate-700 dark:active:bg-slate-600",
  "disabled:pointer-events-none disabled:opacity-50",
);

// Primary click: fit the resolution to the current video area once.
// Caret: a panel (like Paste text) with safe mode, the base size, and the
// auto fit options. Auto fit runs here, so it keeps working while the panel
// is closed.
export default function ResolutionButton({
  onPanelStateChange,
}: {
  onPanelStateChange: (open: boolean) => void;
}) {
  const { send } = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();
  const {
    autoFitResolution,
    setAutoFitResolution,
    autoFitResolutionFullscreen,
    setAutoFitResolutionFullscreen,
    fitBase,
    setFitBase,
    resolutionSafeMode,
    setResolutionSafeMode,
  } = useSettingsStore();
  const { setDisableVideoFocusTrap } = useUiStore();
  const location = useLocation();
  const otherSession = isOtherSessionPath(location.pathname);
  const otherSessionRef = useRef(otherSession);
  otherSessionRef.current = otherSession;

  const [presets, setPresets] = useState<EDIDPreset[]>([]);
  // Lowercased EDID currently set on the device (raw value kept for restoring)
  const [currentEdid, setCurrentEdid] = useState<string | null>(null);
  const rawEdid = useRef<string | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(() => !!document.fullscreenElement);
  // EDID to restore when leaving fullscreen if only fullscreen auto fit is on
  const edidBeforeFullscreen = useRef<string | null>(null);
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
      rawEdid.current = resp.result as string;
      setCurrentEdid((resp.result as string).toLowerCase());
    });
  }, [send]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const fitToWindow = useCallback(
    (base?: FitBase) => {
      if (otherSessionRef.current) return;
      const { clientWidth, clientHeight } = useVideoStore.getState();
      const width = Math.round(clientWidth);
      const height = Math.round(clientHeight);
      if (!width || !height) {
        notifications.error(m.resolution_fit_failed({ error: m.resolution_fit_no_area() }));
        return;
      }
      setBusy(true);
      busyRef.current = true;
      void send(
        "setFitEDID",
        { width, height, base: base ?? useSettingsStore.getState().fitBase },
        (resp: JsonRpcResponse) => {
          setBusy(false);
          busyRef.current = false;
          if ("error" in resp) {
            notifications.error(
              m.resolution_fit_failed({ error: String(resp.error.data || m.unknown_error()) }),
            );
            return;
          }
          lastFitRatio.current = width / height;
          const result = resp.result as { width: number; height: number; delayMs?: number };
          if (result.delayMs && result.delayMs > 0) {
            notifications.success(
              m.resolution_fit_held({ ...result, seconds: Math.ceil(result.delayMs / 1000) }),
            );
            window.setTimeout(refresh, result.delayMs + HELD_CHANGE_REFRESH_MARGIN_MS);
          } else {
            notifications.success(m.resolution_fit_success(result));
          }
          refresh();
        },
      );
    },
    [send, refresh],
  );

  const setSafeMode = (enabled: boolean) => {
    setResolutionSafeMode(enabled);
    if (!enabled) {
      fitToWindow();
      return;
    }
    // An empty EDID restores the default JetKVM EDID.
    setBusy(true);
    void send("setEDID", { edid: "" }, (resp: JsonRpcResponse) => {
      setBusy(false);
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      lastFitRatio.current = null;
      notifications.success(m.resolution_safe_mode_on());
      refresh();
      window.setTimeout(refresh, 10_000 + HELD_CHANGE_REFRESH_MARGIN_MS);
    });
  };

  const selectBase = (base: FitBase) => {
    setFitBase(base);
    fitToWindow(base);
  };

  const fitted = currentEdid ? fittedSize(currentEdid) : null;
  const currentPreset = presets.find(p => p.edid.toLowerCase() === currentEdid);
  const edidKnown = currentEdid !== null;

  // After a page load the device may already carry a fitted EDID: use its
  // mode as the reference so opening the console does not re-fit.
  useEffect(() => {
    if (lastFitRatio.current === null && fitted) {
      lastFitRatio.current = fitted.width / fitted.height;
    }
  }, [fitted]);

  useEffect(() => {
    const onChange = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);

  // Fullscreen-only auto fit: remember the EDID on the way in, put it back on
  // the way out (only if it changed) so the windowed resolution is kept.
  useEffect(() => {
    const settings = useSettingsStore.getState();
    if (settings.resolutionSafeMode || otherSessionRef.current) return;
    if (isFullscreen) {
      if (settings.autoFitResolutionFullscreen && !settings.autoFitResolution) {
        edidBeforeFullscreen.current = rawEdid.current;
      }
      return;
    }
    const saved = edidBeforeFullscreen.current;
    edidBeforeFullscreen.current = null;
    if (!saved || settings.autoFitResolution) return;
    if (saved.toLowerCase() === rawEdid.current?.toLowerCase()) return;
    void send("setEDID", { edid: saved }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      lastFitRatio.current = null;
      notifications.success(m.resolution_restored());
      refresh();
    });
  }, [isFullscreen, send, refresh]);

  const autoFitActive =
    !resolutionSafeMode &&
    !otherSession &&
    (isFullscreen ? autoFitResolutionFullscreen : autoFitResolution);

  useEffect(() => {
    if (!autoFitActive || !edidKnown) return;
    const check = () => {
      if (document.hidden || busyRef.current || otherSessionRef.current) return;
      const { clientWidth, clientHeight } = useVideoStore.getState();
      if (!clientWidth || !clientHeight) return;
      const last = lastFitRatio.current;
      if (last && Math.abs(clientWidth / clientHeight / last - 1) < AUTO_FIT_TOLERANCE) return;
      fitToWindow();
    };
    const first = window.setTimeout(check, AUTO_FIT_SETTLE_MS);
    const id = window.setInterval(check, AUTO_FIT_INTERVAL_MS);
    return () => {
      window.clearTimeout(first);
      window.clearInterval(id);
    };
  }, [autoFitActive, isFullscreen, edidKnown, fitToWindow]);

  const locked = busy || resolutionSafeMode || otherSession;
  const currentLabel = fitted
    ? m.resolution_fit_current(fitted)
    : (currentPreset?.name ?? (edidKnown ? m.video_edid_custom() : "…"));

  return (
    <Popover>
      <SplitButtonGroup>
        <SplitButtonPrimary
          icon={LuMonitor}
          label={m.action_bar_resolution()}
          title={m.resolution_fit_tooltip()}
          disabled={locked}
          onClick={() => fitToWindow()}
        />
        <PopoverButton className={caretClass} onClick={() => setDisableVideoFocusTrap(true)}>
          <ChevronDownIcon className="size-3.5 text-black dark:text-white" />
        </PopoverButton>
      </SplitButtonGroup>
      <PopoverPanel
        anchor="bottom start"
        transition
        className={cx(
          "z-10 flex w-[420px] origin-top flex-col overflow-visible!",
          "flex origin-top flex-col transition duration-300 ease-out data-closed:translate-y-8 data-closed:opacity-0",
        )}
      >
        {({ open }) => {
          onPanelStateChange(open);
          return (
            <div className="mx-auto w-full max-w-xl">
              <GridCard>
                <div className="space-y-4 p-4 py-3">
                  <SettingsPageHeader
                    title={m.action_bar_resolution()}
                    description={m.resolution_panel_description()}
                  />

                  <CheckboxWithLabel
                    label={m.resolution_safe_mode()}
                    description={m.resolution_safe_mode_description()}
                    checked={resolutionSafeMode}
                    disabled={busy}
                    onChange={e => setSafeMode(e.target.checked)}
                  />

                  <fieldset disabled={locked} className="space-y-4 disabled:opacity-50">
                    <div className="space-y-1.5">
                      <div className="text-sm font-semibold text-black dark:text-white">
                        {m.resolution_base()}
                      </div>
                      <div
                        role="radiogroup"
                        className={cx(
                          "flex divide-x divide-slate-800/20 rounded-md border border-slate-800/20",
                          "dark:divide-slate-300/20 dark:border-slate-300/20",
                        )}
                      >
                        {FIT_BASES.map(base => (
                          <label
                            key={base}
                            className="flex flex-1 cursor-pointer items-center justify-center gap-x-2 px-2 py-2 text-sm text-slate-900 dark:text-slate-100"
                          >
                            <input
                              type="radio"
                              name="resolution-fit-base"
                              value={base}
                              checked={fitBase === base}
                              onChange={() => selectBase(base)}
                              className="text-blue-700 focus:ring-blue-700 dark:bg-slate-800"
                            />
                            <span>{base.replace("x", "×")}</span>
                          </label>
                        ))}
                      </div>
                    </div>

                    <div className="flex items-center justify-between gap-x-2">
                      <CheckboxWithLabel
                        label={m.resolution_auto_fit()}
                        description={m.resolution_auto_fit_description()}
                        checked={autoFitResolution}
                        onChange={e => setAutoFitResolution(e.target.checked)}
                      />
                      <Button
                        size="SM"
                        theme="light"
                        text={m.resolution_fit_now()}
                        disabled={locked}
                        onClick={() => fitToWindow()}
                      />
                    </div>
                    <CheckboxWithLabel
                      label={m.resolution_auto_fit_fullscreen()}
                      description={m.resolution_auto_fit_fullscreen_description()}
                      checked={autoFitResolutionFullscreen}
                      onChange={e => setAutoFitResolutionFullscreen(e.target.checked)}
                    />
                  </fieldset>

                  <div className="flex items-center justify-between gap-x-2">
                    <span className="text-xs text-slate-600 dark:text-slate-400">
                      {m.resolution_current({ current: currentLabel })}
                    </span>
                    <Button
                      size="SM"
                      theme="light"
                      text={m.resolution_popover_more_settings()}
                      onClick={() => navigateTo("/settings/video")}
                    />
                  </div>
                </div>
              </GridCard>
            </div>
          );
        }}
      </PopoverPanel>
    </Popover>
  );
}
