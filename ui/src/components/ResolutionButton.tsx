import { useCallback, useEffect, useRef, useState } from "react";
import { LuCheck, LuMonitor } from "react-icons/lu";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";
import { ChevronDownIcon } from "@heroicons/react/16/solid";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useSettingsStore, useUiStore, useVideoStore } from "@hooks/stores";
import { SplitButtonGroup, SplitButtonPrimary } from "@components/SplitButton";
import { Button } from "@components/Button";
import Card, { GridCard } from "@components/Card";
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
// ratio may drift from the last fit before re-fitting. Aligning the mode to
// multiples of 16 alone can change the ratio by up to ~1.8%.
const AUTO_FIT_INTERVAL_MS = 10_000;
const AUTO_FIT_TOLERANCE = 0.03;

// Matches the caret half of SplitButton (which only offers a Menu caret).
const caretClass = cx(
  "inline-flex h-[28px] cursor-pointer items-center rounded-r-sm px-1 select-none",
  "border border-slate-800/30 border-l-slate-800/15 bg-white text-black shadow-xs outline-hidden",
  "dark:border-slate-300/20 dark:border-l-slate-300/10 dark:bg-slate-800 dark:text-white",
  "transition-all duration-200 hover:bg-blue-50/80 active:bg-blue-100/60",
  "dark:hover:bg-slate-700 dark:active:bg-slate-600",
);

// Primary click: set an EDID matching the current video area once.
// Caret: a panel (like Paste text) with the auto fit toggle and the EDID
// presets from the video settings. Auto fit runs here, so it keeps working
// while the panel is closed.
export default function ResolutionButton({
  onPanelStateChange,
}: {
  onPanelStateChange: (open: boolean) => void;
}) {
  const { send } = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();
  const { autoFitResolution, setAutoFitResolution } = useSettingsStore();
  const { setDisableVideoFocusTrap } = useUiStore();

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

  return (
    <Popover>
      <SplitButtonGroup>
        <SplitButtonPrimary
          icon={LuMonitor}
          label={m.action_bar_resolution()}
          title={m.resolution_fit_tooltip()}
          disabled={busy}
          onClick={fitToWindow}
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
                      disabled={busy}
                      onClick={fitToWindow}
                    />
                  </div>
                  <Card className="animate-fadeIn opacity-0">
                    <div className="w-full divide-y divide-slate-700/30 dark:divide-slate-600/30">
                      {presets.map(preset => {
                        const selected = preset.edid.toLowerCase() === currentEdid;
                        return (
                          <button
                            key={preset.edid}
                            type="button"
                            disabled={busy}
                            onClick={() => selectPreset(preset)}
                            className={cx(
                              "flex w-full items-center justify-between gap-x-2 p-3 text-left text-sm",
                              "text-slate-900 hover:bg-slate-100 disabled:opacity-60 dark:text-slate-100 dark:hover:bg-slate-700/50",
                              selected && "font-semibold",
                            )}
                          >
                            <span>{preset.name}</span>
                            {selected && (
                              <LuCheck className="h-4 w-4 shrink-0 text-blue-700 dark:text-blue-500" />
                            )}
                          </button>
                        );
                      })}
                      {currentEdid && !isPreset && (
                        <div className="flex w-full items-center justify-between gap-x-2 p-3 text-sm font-semibold text-slate-900 dark:text-slate-100">
                          <span>
                            {fitted ? m.resolution_fit_current(fitted) : m.video_edid_custom()}
                          </span>
                          <LuCheck className="h-4 w-4 shrink-0 text-blue-700 dark:text-blue-500" />
                        </div>
                      )}
                    </div>
                  </Card>
                  <div className="flex justify-end">
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
