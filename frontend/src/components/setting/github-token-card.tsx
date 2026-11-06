import { Setting } from "@bindings/setting";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const statusKey = ["settings", "github-token"];
const createTokenURL = "https://github.com/settings/personal-access-tokens/new";

export function GitHubTokenCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const status = useQuery({
    queryKey: statusKey,
    queryFn: () => Setting.GetGitHubTokenStatus(),
  });
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    return Events.On("github:token-rejected", () => {
      void queryClient.invalidateQueries({ queryKey: statusKey });
    });
  }, [queryClient]);

  const submit = (action: "replace" | "clear") => {
    if (action === "replace" && !token.trim()) {
      setError("empty");
      return;
    }
    setSaving(true);
    setError("");
    void Setting.SetGitHubToken({ action, token: action === "replace" ? token : "" })
      .then((limit) => {
        setToken("");
        if (action === "replace") {
          toast.success(t("page.setting.network.githubToken.saved", { limit }));
        }
        return queryClient.invalidateQueries({ queryKey: statusKey });
      })
      .catch((reason: unknown) => {
        const message =
          reason instanceof Error ? reason.message : typeof reason === "string" ? reason : "";
        const code = ["invalid", "empty"].find((field) => message.includes(`githubToken.${field}`));
        setError(code ?? "saveFailed");
      })
      .finally(() => setSaving(false));
  };

  const configured = status.data?.configured ?? false;
  return (
    <Section>
      <SectionHeader>
        <SectionTitle>{t("page.setting.network.githubToken.title")}</SectionTitle>
      </SectionHeader>
      <SectionContent>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            submit("replace");
          }}
        >
          <p className="text-xs text-muted-foreground">
            {t("page.setting.network.githubToken.description")}{" "}
            <a href={createTokenURL} target="_blank" rel="noreferrer" className="underline">
              {t("page.setting.network.githubToken.create")}
            </a>
          </p>
          {status.isError && (
            <div role="alert" className="flex items-center gap-2 text-sm text-destructive">
              {t("page.setting.network.loadFailed")}
              <Button type="button" variant="outline" onClick={() => void status.refetch()}>
                {t("page.setting.network.retry")}
              </Button>
            </div>
          )}
          {status.data?.rejected && (
            <p role="alert" className="text-sm text-destructive">
              {t("page.setting.network.githubToken.rejected")}
            </p>
          )}
          {status.data?.unreadable && (
            <p role="alert" className="text-sm text-destructive">
              {t("page.setting.network.githubToken.unreadable")}
            </p>
          )}
          <fieldset disabled={saving || !status.data} className="space-y-4">
            <div className="space-y-1">
              <label htmlFor="github-token">{t("page.setting.network.githubToken.label")}</label>
              <Input
                id="github-token"
                type="password"
                value={token}
                autoComplete="new-password"
                aria-invalid={error === "invalid" || error === "empty"}
                placeholder={
                  configured ? t("page.setting.network.githubToken.savedPlaceholder") : ""
                }
                onChange={(event) => setToken(event.target.value)}
              />
            </div>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {t(`page.setting.network.githubToken.errors.${error}`)}
              </p>
            )}
            <div className="flex gap-2">
              <Button type="submit">
                {t(saving ? "page.setting.network.saving" : "page.setting.network.save")}
              </Button>
              {configured && (
                <Button type="button" variant="outline" onClick={() => submit("clear")}>
                  {t("page.setting.network.githubToken.remove")}
                </Button>
              )}
            </div>
          </fieldset>
        </form>
      </SectionContent>
    </Section>
  );
}
