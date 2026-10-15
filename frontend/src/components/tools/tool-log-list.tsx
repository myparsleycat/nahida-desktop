import { cn } from "@renderer/lib/utils";

// Backend entries look like "[Jan 2, 2006, 3:04:05 PM] [INFO] message".
const logPattern = /^\[(.+?)\] \[(INFO|ERROR)\] ([\s\S]*)$/;

export function ToolLogList({ logs, emptyText }: { logs: string[]; emptyText: string }) {
  if (logs.length === 0) {
    return <p className="text-xs text-muted-foreground">{emptyText}</p>;
  }

  return (
    <ol className="space-y-1.5 rounded-md bg-muted/50 p-2 font-mono text-xs">
      {[...logs].reverse().map((log, index) => {
        const match = logPattern.exec(log);
        if (!match) {
          return (
            <li key={`${log}-${index}`} className="break-all">
              {log}
            </li>
          );
        }

        const isError = match[2] === "ERROR";
        return (
          <li key={`${log}-${index}`} className="flex flex-col gap-x-2 sm:flex-row">
            <span className="shrink-0 text-muted-foreground">{match[1]}</span>
            <span
              className={cn(
                "w-10 shrink-0 font-medium",
                isError ? "text-destructive" : "text-muted-foreground",
              )}
            >
              {match[2]}
            </span>
            <span
              className={cn("min-w-0 break-all whitespace-pre-wrap", isError && "text-destructive")}
            >
              {match[3]}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
