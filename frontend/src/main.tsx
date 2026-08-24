import { Component, ErrorInfo, ReactNode, StrictMode, useEffect, useRef } from "react";
import { createRoot } from "react-dom/client";
import { GUIProbeConfig, ReportFrontendReady, ReportGUIProbe } from "../wailsjs/go/main/App";
import App from "./App";
import { callWhenCoreReady } from "./core_startup";
import { runGUIProbe } from "./gui_probe";
import { RealSlackProbeConfig, runRealSlackProbe } from "./real_slack_probe";
import "./app.css";

type BoundaryState = { error: string };
type GUIProbeConfigValue =
  | { mode: "first-run" | "workbench"; source_commit?: string }
  | RealSlackProbeConfig;

class FrontendErrorBoundary extends Component<{ children: ReactNode }, BoundaryState> {
  state: BoundaryState = { error: "" };

  static getDerivedStateFromError(error: unknown): BoundaryState {
    return { error: String(error) };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("frontend.render", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <main className="loading-shell">
          <div className="brand">CW</div>
          <p role="alert">界面渲染失败：{this.state.error}</p>
        </main>
      );
    }
    return this.props.children;
  }
}

function RootApp() {
  const probeStarted = useRef(false);
  useEffect(() => {
    if (probeStarted.current) return;
    probeStarted.current = true;
    void (async () => {
      let mode: GUIProbeConfigValue["mode"] = "first-run";
      try {
        await callWhenCoreReady(() => ReportFrontendReady("react-mounted-v1"));
        const raw = (await GUIProbeConfig()).trim();
        if (!raw) return;
        const config = JSON.parse(raw) as GUIProbeConfigValue;
        mode = config.mode;
        const result = config.mode === "real-slack"
          ? await runRealSlackProbe(config)
          : await runGUIProbe(config);
        await ReportGUIProbe(JSON.stringify(result));
      } catch (cause) {
        console.error("gui.probe", cause);
        const fallback = { mode, success: false, checks: [], error: String(cause) };
        try { await ReportGUIProbe(JSON.stringify(fallback)); } catch (_) {}
      }
    })();
  }, []);
  return <App />;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <FrontendErrorBoundary>
      <RootApp />
    </FrontendErrorBoundary>
  </StrictMode>,
);
