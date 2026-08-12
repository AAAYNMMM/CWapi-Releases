import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import {
  BLOCKING_END_EVENT,
  BLOCKING_START_EVENT,
  DESKTOP_STATUS_EVENT,
  type BlockingEndDetail,
  type BlockingStartDetail,
  type DesktopStatusDetail,
} from "./blocking-events";

const OVERLAY_DELAY_MS = 120;

export function BlockingUi({ children }: { children: ReactNode }) {
  const [operations, setOperations] = useState<Map<string, string>>(() => new Map());
  const [startupBusy, setStartupBusy] = useState(true);
  const [overlayVisible, setOverlayVisible] = useState(false);
  const contentRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const onStart = (event: Event) => {
      const detail = (event as CustomEvent<BlockingStartDetail>).detail;
      if (!detail?.id) return;
      setOperations((current) => {
        const next = new Map(current);
        next.set(detail.id, detail.message || "正在处理…");
        return next;
      });
    };
    const onEnd = (event: Event) => {
      const detail = (event as CustomEvent<BlockingEndDetail>).detail;
      if (!detail?.id) return;
      setOperations((current) => {
        if (!current.has(detail.id)) return current;
        const next = new Map(current);
        next.delete(detail.id);
        return next;
      });
    };
    const onDesktopStatus = (event: Event) => {
      const status = (event as CustomEvent<DesktopStatusDetail>).detail;
      if (!status) return;
      setStartupBusy(
        !status.setup_required
          && !status.backend_running
          && !status.startup_error,
      );
    };

    window.addEventListener(BLOCKING_START_EVENT, onStart);
    window.addEventListener(BLOCKING_END_EVENT, onEnd);
    window.addEventListener(DESKTOP_STATUS_EVENT, onDesktopStatus);
    return () => {
      window.removeEventListener(BLOCKING_START_EVENT, onStart);
      window.removeEventListener(BLOCKING_END_EVENT, onEnd);
      window.removeEventListener(DESKTOP_STATUS_EVENT, onDesktopStatus);
    };
  }, []);

  const activeMessage = useMemo(() => {
    const messages = Array.from(operations.values());
    return messages[messages.length - 1] ?? "正在启动后台服务…";
  }, [operations]);
  const busy = startupBusy || operations.size > 0;

  useEffect(() => {
    const content = contentRef.current;
    if (content) {
      if (busy) content.setAttribute("inert", "");
      else content.removeAttribute("inert");
    }

    if (!busy) {
      setOverlayVisible(false);
      return;
    }
    const timer = window.setTimeout(() => setOverlayVisible(true), OVERLAY_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [busy]);

  return (
    <>
      <div ref={contentRef} className="blocking-ui-content" aria-busy={busy}>
        {children}
      </div>
      {busy && overlayVisible && (
        <div className="blocking-ui-overlay" role="status" aria-live="polite" aria-label={activeMessage}>
          <div className="blocking-ui-indicator">
            <span className="blocking-ui-spinner" aria-hidden="true" />
            <span>{activeMessage}</span>
          </div>
        </div>
      )}
    </>
  );
}
