import { LoaderIcon } from "lucide-react";

export function SettingsLoading() {
  return (
    <div className="flex min-h-[calc(100vh-8rem)] flex-1 flex-col items-center justify-center space-y-1">
      <LoaderIcon className="size-12 animate-spin text-muted-foreground" />
      {/* <p className="text-lg">Loading</p> */}
    </div>
  );
}
