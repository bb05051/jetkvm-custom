import { useEffect, useState } from "react";
import { LuCheck } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import Card, { GridCard } from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { Button } from "@components/Button";
import { cx } from "@/cva.config";
import notifications from "@/notifications";

interface EDIDPreset {
  name: string;
  edid: string;
}

// Quick access to the EDID presets from the video settings. The target host
// picks its resolution from the advertised EDID.
export default function ResolutionPopover() {
  const { send } = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();

  const [presets, setPresets] = useState<EDIDPreset[]>([]);
  // Lowercased EDID currently set on the device
  const [currentEdid, setCurrentEdid] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    void send("getEDIDPresets", {}, (resp: JsonRpcResponse) => {
      if (!active) return;
      if ("error" in resp) {
        setLoading(false);
        notifications.error(
          m.video_failed_get_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      setPresets(resp.result as EDIDPreset[]);
      void send("getEDID", {}, (resp: JsonRpcResponse) => {
        if (!active) return;
        setLoading(false);
        if ("error" in resp) {
          notifications.error(
            m.video_failed_get_edid({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        setCurrentEdid((resp.result as string).toLowerCase());
      });
    });
    return () => {
      active = false;
    };
  }, [send]);

  const handleSelect = (preset: EDIDPreset) => {
    setLoading(true);
    void send("setEDID", { edid: preset.edid }, (resp: JsonRpcResponse) => {
      setLoading(false);
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      setCurrentEdid(preset.edid.toLowerCase());
      notifications.success(m.video_edid_set_success({ edid: preset.name }));
    });
  };

  const isCustom = currentEdid !== null && !presets.some(p => p.edid.toLowerCase() === currentEdid);

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <SettingsPageHeader
          title={m.action_bar_resolution()}
          description={m.resolution_popover_description()}
        />
        <Card className="animate-fadeIn opacity-0">
          <div className="w-full divide-y divide-slate-700/30 dark:divide-slate-600/30">
            {presets.map(preset => {
              const selected = preset.edid.toLowerCase() === currentEdid;
              return (
                <button
                  key={preset.edid}
                  type="button"
                  disabled={loading}
                  onClick={() => handleSelect(preset)}
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
            {isCustom && (
              <div className="flex w-full items-center justify-between gap-x-2 p-3 text-sm font-semibold text-slate-900 dark:text-slate-100">
                <span>{m.video_edid_custom()}</span>
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
  );
}
