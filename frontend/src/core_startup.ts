const retryDelayMilliseconds = 100;
const startupTimeoutMilliseconds = 30_000;

const delay = (milliseconds: number) => new Promise((resolve) => window.setTimeout(resolve, milliseconds));

export async function callWhenCoreReady<T>(operation: () => Promise<T>): Promise<T> {
  const deadline = Date.now() + startupTimeoutMilliseconds;
  let lastError: unknown = new Error("CORE_STARTUP_TIMEOUT");
  while (Date.now() < deadline) {
    try {
      return await operation();
    } catch (cause) {
      lastError = cause;
      if (!String(cause).includes("CORE_NOT_STARTED")) throw cause;
      await delay(retryDelayMilliseconds);
    }
  }
  throw lastError;
}
