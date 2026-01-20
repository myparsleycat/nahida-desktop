// import { StrictMode } from 'react'
import ReactDOM from "react-dom/client";
import "./index.css";
import "@renderer/lib/i18n";

// Import the generated route tree
import { setupWindowControls } from "./window-controls";
import { RouterProvider, createRouter, createHashHistory } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";

setupWindowControls();

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

// Render the app
const rootElement = document.getElementById("root")!;
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}
