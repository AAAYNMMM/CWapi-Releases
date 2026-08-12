import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { BlockingUi } from "./blocking-ui";
import "./styles.css";
import "./stage8.css";
import "./stage9-console-fix.css";
import "./blocking-ui.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BlockingUi>
      <App />
    </BlockingUi>
  </React.StrictMode>,
);
