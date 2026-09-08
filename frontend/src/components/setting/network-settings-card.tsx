import { Window as AppWindow } from "@bindings/app";
import { Setting, type ProxySettings, type ProxySettingsInput } from "@bindings/setting";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

export function NetworkSettingsCard() {
  const { t } = useTranslation();
  const query = useQuery({
    queryKey: ["settings", "proxy"],
    queryFn: () => Setting.GetProxySettings(),
  });
  if (query.isPending) return <p role="status">{t("page.setting.network.loading")}</p>;
  if (!query.data)
    return (
      <div role="alert">
        {t("page.setting.network.loadFailed")}
        <Button onClick={() => void query.refetch()}>{t("page.setting.network.retry")}</Button>
      </div>
    );
  return <NetworkSettingsForm initial={query.data} />;
}

function NetworkSettingsForm({ initial }: { initial: ProxySettings }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [saved, setSaved] = useState(initial);
  const [draft, setDraft] = useState<ProxySettingsInput>({
    enabled: initial.enabled,
    type: initial.type,
    host: initial.host,
    port: initial.port,
    username: initial.username,
    password: "",
    passwordAction: initial.configurationInvalid ? "clear" : "keep",
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [restartOpen, setRestartOpen] = useState(false);
  const [restartError, setRestartError] = useState(false);
  const validation = proxyDraftError(draft, saved);
  const save = () => {
    if (validation) {
      setError(validation);
      return;
    }
    setSaving(true);
    setError("");
    void Setting.SetProxySettings(draft)
      .then(() => Setting.GetProxySettings())
      .then((result) => {
        setSaved(result);
        setDraft((previous) => ({ ...previous, password: "", passwordAction: "keep" }));
        queryClient.setQueryData(["settings", "proxy"], result);
        if (result.restartRequired) {
          setRestartError(false);
          setRestartOpen(true);
        }
      })
      .catch((reason: unknown) => {
        const message =
          reason instanceof Error ? reason.message : typeof reason === "string" ? reason : "";
        const code = [
          "host",
          "port",
          "username",
          "credentials",
          "passwordResetRequired",
          "passwordAction",
          "type",
        ].find((field) => message.includes(`proxy.${field}`));
        setError(code ?? "saveFailed");
      })
      .finally(() => setSaving(false));
  };
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">{t("page.setting.network.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              save();
            }}
          >
            {saved.configurationInvalid && (
              <p role="alert" className="text-sm text-destructive">
                {t("page.setting.network.invalid")}
              </p>
            )}
            {saved.restartRequired && (
              <p role="status" className="text-sm text-muted-foreground">
                {t("page.setting.network.restart")}
              </p>
            )}
            <p className="text-xs text-muted-foreground">{t("page.setting.network.description")}</p>
            <fieldset disabled={saving} className="space-y-4">
              <div className="flex items-center justify-between">
                <label htmlFor="proxy-enabled">{t("page.setting.network.enabled")}</label>
                <Switch
                  id="proxy-enabled"
                  checked={draft.enabled}
                  onCheckedChange={(enabled) => setDraft((previous) => ({ ...previous, enabled }))}
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="proxy-type">{t("page.setting.network.type")}</label>
                <Select
                  value={draft.type}
                  onValueChange={(type) => {
                    if (type) setDraft((previous) => ({ ...previous, type }));
                  }}
                >
                  <SelectTrigger id="proxy-type">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {[
                        ["http", "HTTP"],
                        ["socks5", "SOCKS5"],
                        ["socks5h", "SOCKS5h"],
                      ].map(([value, label]) => (
                        <SelectItem key={value} value={value}>
                          {label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">{t("page.setting.network.dns")}</p>
              </div>
              <div className="space-y-1">
                <label htmlFor="proxy-host">{t("page.setting.network.host")}</label>
                <Input
                  id="proxy-host"
                  value={draft.host}
                  autoComplete="off"
                  aria-invalid={error === "host"}
                  onChange={(event) =>
                    setDraft((previous) => ({ ...previous, host: event.target.value }))
                  }
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="proxy-port">{t("page.setting.network.port")}</label>
                <Input
                  id="proxy-port"
                  type="number"
                  min={draft.enabled ? 1 : 0}
                  max={65535}
                  step={1}
                  value={draft.port || ""}
                  aria-invalid={error === "port"}
                  onChange={(event) =>
                    setDraft((previous) => ({ ...previous, port: Number(event.target.value) }))
                  }
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="proxy-username">{t("page.setting.network.username")}</label>
                <Input
                  id="proxy-username"
                  value={draft.username}
                  autoComplete="off"
                  aria-invalid={error === "username"}
                  onChange={(event) =>
                    setDraft((previous) => ({ ...previous, username: event.target.value }))
                  }
                />
              </div>
              <div className="space-y-1">
                <label htmlFor="proxy-password">{t("page.setting.network.password")}</label>
                <Input
                  id="proxy-password"
                  type="password"
                  value={draft.password}
                  autoComplete="new-password"
                  placeholder={
                    saved.hasPassword && draft.passwordAction === "keep"
                      ? t("page.setting.network.passwordSaved")
                      : ""
                  }
                  onChange={(event) =>
                    setDraft((previous) => ({
                      ...previous,
                      password: event.target.value,
                      passwordAction: "replace",
                    }))
                  }
                />
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() =>
                      setDraft((previous) => ({
                        ...previous,
                        password: "",
                        passwordAction: "clear",
                      }))
                    }
                  >
                    {t("page.setting.network.clearPassword")}
                  </Button>
                  {saved.hasPassword && (
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() =>
                        setDraft((previous) => ({
                          ...previous,
                          password: "",
                          passwordAction: "keep",
                        }))
                      }
                    >
                      {t("page.setting.network.keepPassword")}
                    </Button>
                  )}
                </div>
                {draft.passwordAction === "clear" && (
                  <p className="text-xs text-muted-foreground">
                    {t("page.setting.network.passwordWillClear")}
                  </p>
                )}
              </div>
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {t(`page.setting.network.errors.${error}`)}
                </p>
              )}
              <Button type="submit" disabled={saving}>
                {t(saving ? "page.setting.network.saving" : "page.setting.network.save")}
              </Button>
            </fieldset>
          </form>
        </CardContent>
      </Card>
      <AlertDialog open={restartOpen} onOpenChange={setRestartOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.setting.network.restartDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>{t("page.setting.network.restart")}</AlertDialogDescription>
          </AlertDialogHeader>
          {restartError && (
            <p role="alert" className="text-sm text-destructive">
              {t("page.setting.network.restartDialog.failed")}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClickPromise={() =>
                AppWindow.Restart().catch(() => {
                  setRestartError(true);
                })
              }
            >
              {t("page.setting.network.restartDialog.action")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function proxyDraftError(draft: ProxySettingsInput, saved: ProxySettings) {
  if (!draft.enabled) {
    return "";
  }
  if (!draft.host.trim() || /[\s/\\@?#]/u.test(draft.host.trim())) {
    return "host";
  }
  if (!Number.isInteger(draft.port) || draft.port < 1 || draft.port > 65535) {
    return "port";
  }
  const needsUsername =
    draft.passwordAction === "replace"
      ? draft.password !== ""
      : draft.passwordAction === "keep" && saved.hasPassword;
  if (!draft.username && needsUsername) {
    return "username";
  }
  return "";
}
