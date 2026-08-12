export const BLOCKING_START_EVENT = "cwapi:blocking-start";
export const BLOCKING_END_EVENT = "cwapi:blocking-end";
export const DESKTOP_STATUS_EVENT = "cwapi:desktop-status";

export type BlockingStartDetail = {
  id: string;
  message: string;
};

export type BlockingEndDetail = {
  id: string;
};

export type DesktopStatusDetail = {
  backend_running: boolean;
  setup_required: boolean;
  startup_error: string | null;
};

let nextBlockingId = 0;

export function beginBlockingOperation(message: string): string {
  nextBlockingId += 1;
  const id = `blocking-${nextBlockingId}`;
  window.dispatchEvent(
    new CustomEvent<BlockingStartDetail>(BLOCKING_START_EVENT, {
      detail: { id, message },
    }),
  );
  return id;
}

export function endBlockingOperation(id: string): void {
  window.dispatchEvent(
    new CustomEvent<BlockingEndDetail>(BLOCKING_END_EVENT, {
      detail: { id },
    }),
  );
}

export function publishDesktopStatus(status: DesktopStatusDetail): void {
  window.dispatchEvent(
    new CustomEvent<DesktopStatusDetail>(DESKTOP_STATUS_EVENT, {
      detail: status,
    }),
  );
}
