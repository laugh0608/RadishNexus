import { useState } from "react";
import { NexusView } from "./nexus-view/NexusView";
import {
  decisionNexusViewFixture,
  emptyDecisionNexusViewFixture,
} from "./nexus-view/fixture";
import type { NexusViewState } from "./nexus-view/model";

type PrototypeMode = "data" | "loading" | "empty" | "error";

const prototypeStates: Record<PrototypeMode, NexusViewState> = {
  data: { status: "ready", data: decisionNexusViewFixture },
  loading: { status: "loading" },
  empty: { status: "ready", data: emptyDecisionNexusViewFixture },
  error: {
    status: "error",
    message:
      "服务没有返回可用的 Nexus View。请稍后重试；已载入的页面不会被当作成功。",
  },
};

const prototypeModes: readonly { id: PrototypeMode; label: string }[] = [
  { id: "data", label: "数据" },
  { id: "loading", label: "加载" },
  { id: "empty", label: "空态" },
  { id: "error", label: "错误" },
];

export function App() {
  const [mode, setMode] = useState<PrototypeMode>("data");

  return (
    <div className="app-shell">
      <header className="prototype-header">
        <a
          className="brand-lockup"
          href="#nexus-current-title"
          aria-label="RadishNexus Nexus View"
        >
          <span className="brand-mark" aria-hidden="true">
            R
          </span>
          <span>
            <strong>RadishNexus</strong>
            <small>Context stays connected.</small>
          </span>
        </a>

        <div className="prototype-note">
          <span aria-hidden="true" />
          静态代表原型 · 非真实工作区数据
        </div>

        <div className="state-switcher" aria-label="原型状态检视">
          <span>状态检视</span>
          <div>
            {prototypeModes.map((item) => (
              <button
                aria-pressed={mode === item.id}
                key={item.id}
                onClick={() => setMode(item.id)}
                type="button"
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
      </header>

      <NexusView
        state={prototypeStates[mode]}
        onRetry={() => setMode("data")}
      />

      <footer className="prototype-footer">
        <span>Representative slice / M0</span>
        <span>Current · Relations · Timeline</span>
      </footer>
    </div>
  );
}
