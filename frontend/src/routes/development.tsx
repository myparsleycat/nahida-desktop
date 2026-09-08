import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

export const Route = createFileRoute("/development")({
  component: RouteComponent,
});

function RouteComponent() {
  const [shouldThrow, setShouldThrow] = useState(false);

  if (shouldThrow) {
    throw new Error("Intentional runtime error");
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-background text-foreground">
      <header className="flex h-10 shrink-0 items-center border-b border-border px-4">
        <span className="font-mono text-xs font-medium text-foreground">Development</span>
      </header>

      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto w-full max-w-xl space-y-6 p-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm font-medium">Runtime error</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="flex items-center justify-between gap-6">
                <p className="text-xs text-muted-foreground">
                  Throw an intentional render error to exercise the app error boundary.
                </p>
                <Button type="button" variant="destructive" onClick={() => setShouldThrow(true)}>
                  Throw error
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      </ScrollArea>
    </div>
  );
}
