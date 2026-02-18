import { LoaderIcon } from "lucide-react";

export function SettingsLoading() {
  return (
    <div className="flex-1 flex flex-col space-y-1 items-center justify-center min-h-[calc(100vh-8rem)]">
      <LoaderIcon className="size-12 animate-spin text-muted-foreground" />
      {/* <p className="text-lg">Loading</p> */}
    </div>
  );
}
