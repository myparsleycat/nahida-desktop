import { CDNTrace } from "@bindings/infra";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
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

          <CDNTraceCard />
        </div>
      </ScrollArea>
    </div>
  );
}

function CDNTraceCard() {
  const query = useQuery({
    queryKey: ["development:cdn-cgi-trace"],
    queryFn: () => CDNTrace.Get(),
    refetchOnWindowFocus: false,
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">cdn-cgi/trace</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center justify-between gap-6">
          <p className="text-xs text-muted-foreground">
            Cloudflare trace for the app HTTP client against the Nahida API origin.
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {query.isFetching ? <Loader2Icon className="size-3.5 animate-spin" /> : null}
            Refresh
          </Button>
        </div>
        {query.isError ? (
          <p className="text-xs text-destructive">{toErrorMessage(query.error)}</p>
        ) : query.isPending ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2Icon className="size-3.5 animate-spin" />
            Fetching trace
          </div>
        ) : (
          <CDNTraceDocument text={query.data ?? ""} />
        )}
      </CardContent>
    </Card>
  );
}

function CDNTraceDocument({ text }: { text: string }) {
  return <pre className="overflow-x-auto font-mono text-xs whitespace-pre-wrap">{text}</pre>;
}
