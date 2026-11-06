import { GitHubTokenCard } from "@renderer/components/setting/github-token-card";
import { NetworkSettingsCard } from "@renderer/components/setting/network-settings-card";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/network")({
  component: () => (
    <div className="space-y-6 p-4">
      <NetworkSettingsCard />
      <GitHubTokenCard />
    </div>
  ),
});
