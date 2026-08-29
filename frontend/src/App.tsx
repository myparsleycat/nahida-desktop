import { useCallback, useEffect, useState } from "react";

import type { Session } from "../bindings/nahida.live/desktop/internal/auth/models.js";
import type { FrontendBootstrap } from "./port/bootstrap";

import { loadWailsFrontendBootstrap, onAuthUpdate, onBackendStatus } from "./port/wails";

type State =
  | { status: "loading" }
  | { status: "ready"; bootstrap: FrontendBootstrap<Session> }
  | { status: "error"; error: Error };

export default function App() {
  const [state, setState] = useState<State>({ status: "loading" });

  const load = useCallback(async () => {
    try {
      setState({ status: "ready", bootstrap: await loadWailsFrontendBootstrap() });
    } catch (error) {
      setState({
        status: "error",
        error: error instanceof Error ? error : new Error(String(error)),
      });
    }
  }, []);

  useEffect(() => {
    queueMicrotask(() => void load());
    const offAuth = onAuthUpdate(() => void load());
    const offBackend = onBackendStatus(() => void load());
    return () => {
      offAuth();
      offBackend();
    };
  }, [load]);

  if (state.status === "error") {
    return (
      <main className="port-status">
        <h1>Nahida Desktop</h1>
        <p>초기 상태를 불러오지 못했습니다.</p>
        <pre>{state.error.message}</pre>
        <button
          type="button"
          onClick={() => {
            setState({ status: "loading" });
            void load();
          }}
        >
          다시 시도
        </button>
      </main>
    );
  }

  if (state.status === "loading") {
    return (
      <main className="port-status">
        <h1>Nahida Desktop</h1>
        <p>Loading...</p>
      </main>
    );
  }

  return (
    <main className="port-status" data-start-page={state.bootstrap.startPage}>
      <h1>Nahida Desktop</h1>
      <p>Wails frontend bootstrap is ready.</p>
      <dl>
        <dt>Start page</dt>
        <dd>{state.bootstrap.startPage}</dd>
        <dt>Backend</dt>
        <dd>{state.bootstrap.backendStatus}</dd>
      </dl>
    </main>
  );
}
