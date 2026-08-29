import "@renderer/wails/bridge";
import "@renderer/lib/i18n";
import { Window as AppWindow } from "@bindings/app";
import { Shell } from "@bindings/platform";
import { installExternalWindowHandler } from "@renderer/wails/external-window";
import { TITLE_BAR_HEIGHT } from "@shared/const";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createHashHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { useEffect, useState } from "react";
import ReactDOM from "react-dom/client";

import "./index.css";
import { routeTree } from "./routeTree.gen";

document.documentElement.style.setProperty("--app-titlebar-height", `${TITLE_BAR_HEIGHT}px`);
installExternalWindowHandler(Shell.OpenExternal);

const hashHistory = createHashHistory();
const queryClient = new QueryClient();
const router = createRouter({
  routeTree,
  context: {
    queryClient,
  },
  scrollRestoration: true,
  defaultPreload: "intent",
  history: hashHistory,
});

router.subscribe("onResolved", ({ toLocation }) => {
  void AppWindow.SyncRoute(toLocation.href);
});

// Register the router instance for type safety
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const Root = () => {
  const [key, setKey] = useState(0);

  useEffect(() => {
    const off = Events.On("renderer:reload", () => {
      setKey((prev) => prev + 1);
    });
    return off;
  }, []);

  return (
    // <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider key={key} router={router} />
    </QueryClientProvider>
    // </StrictMode>
  );
};

// Render the app
// biome-ignore lint/style/noNonNullAssertion: <>
const rootElement = document.getElementById("root")!;
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(<Root />);
}
