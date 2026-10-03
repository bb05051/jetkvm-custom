import { useCallback, useEffect, useRef } from "react";

import { useJsonRpc } from "./useJsonRpc";
import { useHidRpc } from "./useHidRpc";
import { useMouseStore, useUiStore } from "./stores";
import { calcAbsMousePosition } from "./useMouse";
import type { TouchContact } from "./hidRpc";

// Direct-touch gestures on the video element:
//   tap               -> left click at the touched point
//   long press        -> right click at the touched point
//   one-finger drag   -> left-button drag
//   two-finger drag   -> scroll wheel
//   two-finger tap    -> right click at the current cursor position
// Touch input always uses absolute positioning, regardless of the mouse mode.
//
// When the USB touchscreen device is enabled, gestures are not interpreted
// here: up to five contacts are forwarded as-is and the target OS handles
// taps, scrolling and pinch zoom natively.

const LONG_PRESS_MS = 500;
const DRAG_THRESHOLD_PX = 10;
const SCROLL_STEP_PX = 20;
const TWO_FINGER_TAP_MS = 250;
// Browsers may emit compatibility mouse events after a touch; ignore those.
const TOUCH_MOUSE_SUPPRESS_MS = 1000;

const BUTTON_NONE = 0;
const BUTTON_LEFT = 1;
const BUTTON_RIGHT = 2;

// Keep in sync with TouchscreenMaxContacts in internal/usbgadget/hid_touchscreen.go
const MAX_TOUCH_CONTACTS = 5;
// Win8-class multitouch hosts (Linux hid-multitouch with sticky-finger
// handling) release contacts that go ~100ms without a report, so resend the
// current contacts while a finger is held still.
const TOUCH_KEEPALIVE_MS = 50;

type Gesture = "idle" | "pending" | "drag" | "scroll" | "done";

interface Point {
  x: number;
  y: number;
}

export default function useTouch() {
  const { setMousePosition } = useMouseStore();
  const { send } = useJsonRpc();
  const { reportAbsMouseEvent, reportTouchscreenEvent, rpcHidReady } = useHidRpc();
  const { usbTouchscreenEnabled } = useUiStore();
  // The touchscreen report only exists on the HID RPC channel.
  const nativeTouch = usbTouchscreenEnabled && rpcHidReady;

  const pointers = useRef(new Map<number, Point>());
  const gesture = useRef<Gesture>("idle");
  const startClient = useRef<Point>({ x: 0, y: 0 });
  const startAbs = useRef<Point>({ x: 0, y: 0 });
  const lastAbs = useRef<Point>({ x: 0, y: 0 });
  const longPressTimer = useRef<number | null>(null);
  const twoFingerStartTime = useRef(0);
  const scrollMoved = useRef(false);
  const scrollAccum = useRef<Point>({ x: 0, y: 0 });
  const lastCentroid = useRef<Point>({ x: 0, y: 0 });
  const lastTouchTime = useRef(0);
  const contacts = useRef(new Map<number, TouchContact>());
  const nextContactId = useRef(0);

  const clearLongPress = useCallback(() => {
    if (longPressTimer.current !== null) {
      window.clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  }, []);

  useEffect(() => clearLongPress, [clearLongPress]);

  const touchKeepAliveTimer = useRef<number | null>(null);
  const reportTouchscreenEventRef = useRef(reportTouchscreenEvent);
  useEffect(() => {
    reportTouchscreenEventRef.current = reportTouchscreenEvent;
  }, [reportTouchscreenEvent]);

  const stopTouchKeepAlive = useCallback(() => {
    if (touchKeepAliveTimer.current !== null) {
      window.clearInterval(touchKeepAliveTimer.current);
      touchKeepAliveTimer.current = null;
    }
  }, []);

  useEffect(() => stopTouchKeepAlive, [stopTouchKeepAlive]);

  const sendAbs = useCallback(
    ({ x, y }: Point, buttons: number) => {
      if (rpcHidReady) {
        reportAbsMouseEvent(x, y, buttons);
      } else {
        // kept for backward compatibility
        send("absMouseReport", { x, y, buttons });
      }
      setMousePosition(x, y);
      lastAbs.current = { x, y };
    },
    [send, reportAbsMouseEvent, rpcHidReady, setMousePosition],
  );

  const click = useCallback(
    (pos: Point, button: number) => {
      sendAbs(pos, button);
      sendAbs(pos, BUTTON_NONE);
    },
    [sendAbs],
  );

  const toAbs = useCallback((e: PointerEvent): Point | null => {
    const video = e.currentTarget as HTMLVideoElement;
    if (!video.clientWidth || !video.clientHeight || !video.videoWidth || !video.videoHeight) {
      return null;
    }
    const rect = video.getBoundingClientRect();
    return calcAbsMousePosition(e.clientX - rect.left, e.clientY - rect.top, {
      videoClientWidth: video.clientWidth,
      videoClientHeight: video.clientHeight,
      videoWidth: video.videoWidth,
      videoHeight: video.videoHeight,
    });
  }, []);

  const centroid = useCallback((): Point => {
    let x = 0;
    let y = 0;
    for (const p of pointers.current.values()) {
      x += p.x;
      y += p.y;
    }
    const n = pointers.current.size || 1;
    return { x: x / n, y: y / n };
  }, []);

  // Sends the active contacts plus any contacts lifted in this frame (tip off).
  const sendTouchFrame = useCallback(
    (lifted: TouchContact[], contactsChanged: boolean) => {
      const frame = [...contacts.current.values(), ...lifted].slice(0, MAX_TOUCH_CONTACTS);
      reportTouchscreenEvent(frame, contactsChanged);

      if (contacts.current.size === 0) {
        stopTouchKeepAlive();
      } else if (touchKeepAliveTimer.current === null) {
        touchKeepAliveTimer.current = window.setInterval(() => {
          const active = [...contacts.current.values()].slice(0, MAX_TOUCH_CONTACTS);
          if (active.length === 0) return;
          reportTouchscreenEventRef.current(active, false);
        }, TOUCH_KEEPALIVE_MS);
      }
    },
    [reportTouchscreenEvent, stopTouchKeepAlive],
  );

  const isRecentTouch = useCallback(
    () => performance.now() - lastTouchTime.current < TOUCH_MOUSE_SUPPRESS_MS,
    [],
  );

  const onPointerDown = useCallback(
    (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;
      e.preventDefault();
      lastTouchTime.current = performance.now();

      try {
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
      } catch {
        // ignore errors
      }
      if (nativeTouch) {
        if (contacts.current.size >= MAX_TOUCH_CONTACTS) return;
        const pos = toAbs(e);
        if (!pos) return;
        contacts.current.set(e.pointerId, {
          id: nextContactId.current,
          tip: true,
          ...pos,
        });
        nextContactId.current = (nextContactId.current + 1) & 0x7f;
        sendTouchFrame([], true);
        return;
      }

      pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });

      if (pointers.current.size === 1) {
        const pos = toAbs(e);
        if (!pos) {
          gesture.current = "done";
          return;
        }
        gesture.current = "pending";
        startClient.current = { x: e.clientX, y: e.clientY };
        startAbs.current = pos;
        // Move the cursor under the finger so hover effects show up.
        sendAbs(pos, BUTTON_NONE);

        clearLongPress();
        longPressTimer.current = window.setTimeout(() => {
          longPressTimer.current = null;
          if (gesture.current !== "pending") return;
          click(startAbs.current, BUTTON_RIGHT);
          navigator.vibrate?.(30);
          gesture.current = "done";
        }, LONG_PRESS_MS);
      } else if (pointers.current.size === 2) {
        clearLongPress();
        if (gesture.current === "drag") sendAbs(lastAbs.current, BUTTON_NONE);
        if (gesture.current === "done") return;
        gesture.current = "scroll";
        twoFingerStartTime.current = performance.now();
        scrollMoved.current = false;
        scrollAccum.current = { x: 0, y: 0 };
        lastCentroid.current = centroid();
      } else {
        gesture.current = "done";
      }
    },
    [nativeTouch, toAbs, sendAbs, sendTouchFrame, click, clearLongPress, centroid],
  );

  const onPointerMove = useCallback(
    (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;

      const contact = contacts.current.get(e.pointerId);
      if (contact) {
        lastTouchTime.current = performance.now();
        const pos = toAbs(e);
        if (!pos || (pos.x === contact.x && pos.y === contact.y)) return;
        contacts.current.set(e.pointerId, { ...contact, ...pos });
        sendTouchFrame([], false);
        return;
      }

      if (!pointers.current.has(e.pointerId)) return;
      lastTouchTime.current = performance.now();
      pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });

      if (gesture.current === "pending") {
        const dist = Math.hypot(
          e.clientX - startClient.current.x,
          e.clientY - startClient.current.y,
        );
        if (dist < DRAG_THRESHOLD_PX) return;
        clearLongPress();
        gesture.current = "drag";
        sendAbs(startAbs.current, BUTTON_LEFT);
      }

      if (gesture.current === "drag") {
        const pos = toAbs(e);
        if (pos) sendAbs(pos, BUTTON_LEFT);
        return;
      }

      if (gesture.current === "scroll") {
        const c = centroid();
        scrollAccum.current.x += c.x - lastCentroid.current.x;
        scrollAccum.current.y += c.y - lastCentroid.current.y;
        lastCentroid.current = c;

        const stepsX = Math.trunc(scrollAccum.current.x / SCROLL_STEP_PX);
        const stepsY = Math.trunc(scrollAccum.current.y / SCROLL_STEP_PX);
        if (stepsX === 0 && stepsY === 0) return;
        scrollMoved.current = true;
        scrollAccum.current.x -= stepsX * SCROLL_STEP_PX;
        scrollAccum.current.y -= stepsY * SCROLL_STEP_PX;

        // Natural scrolling: content follows the fingers. HID wheel positive = up/right.
        const clamp = (v: number) => Math.max(-127, Math.min(127, v));
        send("wheelReport", { wheelY: clamp(stepsY), wheelX: clamp(-stepsX) });
      }
    },
    [toAbs, sendAbs, sendTouchFrame, send, clearLongPress, centroid],
  );

  const onPointerEnd = useCallback(
    (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;

      const contact = contacts.current.get(e.pointerId);
      if (contact) {
        lastTouchTime.current = performance.now();
        contacts.current.delete(e.pointerId);
        sendTouchFrame([{ ...contact, tip: false }], true);
        return;
      }

      if (!pointers.current.has(e.pointerId)) return;
      lastTouchTime.current = performance.now();
      pointers.current.delete(e.pointerId);
      const cancelled = e.type === "pointercancel";

      switch (gesture.current) {
        case "pending":
          clearLongPress();
          if (!cancelled) click(startAbs.current, BUTTON_LEFT);
          gesture.current = "done";
          break;
        case "drag":
          sendAbs(toAbs(e) ?? lastAbs.current, BUTTON_NONE);
          gesture.current = "done";
          break;
        case "scroll":
          if (
            !cancelled &&
            !scrollMoved.current &&
            performance.now() - twoFingerStartTime.current < TWO_FINGER_TAP_MS
          ) {
            click(lastAbs.current, BUTTON_RIGHT);
          }
          gesture.current = "done";
          break;
      }

      if (pointers.current.size === 0) gesture.current = "idle";
    },
    [toAbs, sendAbs, sendTouchFrame, click, clearLongPress],
  );

  return { onPointerDown, onPointerMove, onPointerEnd, isRecentTouch };
}
