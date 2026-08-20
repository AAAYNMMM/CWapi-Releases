import { useEffect, useRef } from "react";
import { durationText, formatTime, LogEntry, statusText, statusTone } from "../app_model";

export function StatusDot({ label, state }: { label: string; state: unknown }) {
  return (
    <div className="dashboard-status-item" title={`${label}: ${statusText(state)}`}>
      <span className={`status-dot status-dot-${statusTone(state)}`} aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

export function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="detail-row">
      <span>{label}</span>
      <strong className={mono ? "mono" : ""}>{value || "—"}</strong>
    </div>
  );
}

export function LogSurface({
  title,
  subtitle,
  testId,
  entries,
  fontSize,
  autoFollow,
}: {
  title: string;
  subtitle: string;
  testId: string;
  entries: LogEntry[];
  fontSize: number;
  autoFollow: boolean;
}) {
  const viewport = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (autoFollow && viewport.current) viewport.current.scrollTop = viewport.current.scrollHeight;
  }, [entries, autoFollow]);

  return (
    <section className="panel log-panel" data-testid={testId}>
      <div className="panel-title-row">
        <div>
          <h2>{title}</h2>
          <p className="log-panel-subtitle">{subtitle}</p>
        </div>
      </div>
      <div ref={viewport} className="log-viewport" style={{ fontSize }}>
        {entries.length ? entries.map((entry) => (
          <div className="structured-log-row" key={entry.key}>
            <span className={`structured-state-dot structured-state-${statusTone(entry.status)}`} aria-hidden="true" />
            <div className="structured-main">
              <strong>{entry.head}</strong>
              <span>{entry.message}</span>
            </div>
            <span className="structured-status">{entry.status ? statusText(entry.status) : ""}</span>
            <time>{entry.duration ? durationText(entry.duration) : formatTime(entry.time)}</time>
          </div>
        )) : <div className="empty-state">当前没有日志。</div>}
      </div>
    </section>
  );
}
