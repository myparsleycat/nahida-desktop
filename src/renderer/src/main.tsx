import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createHashHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import "./index.css";
import "@renderer/lib/i18n";
import { StrictMode, useEffect, useState } from "react";
import ReactDOM from "react-dom/client";
import { routeTree } from "./routeTree.gen";

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

// Register the router instance for type safety
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const Root = () => {
  const [key, setKey] = useState(0);

  useEffect(() => {
    const cleanup = window.api.on("renderer:reload", () => {
      setKey((prev) => prev + 1);
    });
    return cleanup;
  }, []);

  return (
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider key={key} router={router} />
      </QueryClientProvider>
    </StrictMode>
  );
};

// Render the app
// biome-ignore lint/style/noNonNullAssertion: <>
const rootElement = document.getElementById("root")!;
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(<Root />);
}
