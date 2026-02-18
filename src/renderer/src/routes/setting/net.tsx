import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent } from "@renderer/components/ui/card";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { useSettings } from "@renderer/hooks/use-settings";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/net")({
  component: RouteComponent,
});

const settingsConfig = {
  proxy: "setting:net:getProxy",
} as const;

function RouteComponent() {
  const { t } = useTranslation();
  const [parent, _enableAnimations] = useAutoAnimate();

  const { settings, setSettings, isLoading } = useSettings<{
    proxy?: {
      type: string;
      host: string;
      port: string;
      requiresAuth: boolean;
      username?: string;
      password?: string;
    };
  }>(settingsConfig);

  if (isLoading) {
    return null;
  }

  const proxy = settings.proxy || {
    type: "disabled",
    host: "",
    port: "",
    requiresAuth: false,
    username: "",
    password: "",
  };

  const updateProxy = (updates: Partial<typeof proxy>) => {
    setSettings((prev) => ({
      ...prev,
      proxy: { ...proxy, ...updates },
    }));
  };

  const handleSave = async () => {
    await window.api.invoke("setting:net:setProxy", {
      ...proxy,
      username: proxy.requiresAuth ? proxy.username : undefined,
      password: proxy.requiresAuth ? proxy.password : undefined,
    });
    toast.success(t("page.setting.net.proxy.saved"));
  };

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <Card className="w-full">
        <CardContent className="space-y-4" ref={parent}>
          <div className="space-y-2">
            <Label htmlFor="proxy-type">{t("page.setting.net.proxy.type")}</Label>
            <Select value={proxy.type} onValueChange={(val) => updateProxy({ type: val })}>
              <SelectTrigger id="proxy-type">
                <SelectValue placeholder="Select proxy type" />
              </SelectTrigger>
              <SelectContent position="popper" onCloseAutoFocus={(e) => e.preventDefault()}>
                <SelectGroup>
                  <SelectItem value="disabled">{t("page.setting.net.proxy.disabled")}</SelectItem>
                  <SelectItem value="https">HTTPS</SelectItem>
                  <SelectItem value="socks5">SOCKS5</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2 space-y-2">
              <Label htmlFor="host">{t("page.setting.net.proxy.host")}</Label>
              <Input
                id="host"
                placeholder="proxy.example.com"
                value={proxy.host}
                onChange={(e) => updateProxy({ host: e.target.value })}
                disabled={proxy.type === "disabled"}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="port">{t("page.setting.net.proxy.port")}</Label>
              <Input
                id="port"
                placeholder="8080"
                value={proxy.port}
                onChange={(e) => updateProxy({ port: e.target.value })}
                disabled={proxy.type === "disabled"}
              />
            </div>
          </div>

          <div className="flex items-center space-x-2 pt-2">
            <Checkbox
              id="requires-auth"
              checked={proxy.requiresAuth}
              onCheckedChange={(checked) => updateProxy({ requiresAuth: checked === true })}
              disabled={proxy.type === "disabled"}
            />
            <Label htmlFor="requires-auth" className="cursor-pointer font-normal">
              {t("page.setting.net.proxy.requiresAuth")}
            </Label>
          </div>

          {proxy.requiresAuth && (
            <div className="space-y-4 pt-2 border-t">
              <div className="space-y-2 pt-4">
                <Label htmlFor="username">{t("page.setting.net.proxy.username")}</Label>
                <Input
                  id="username"
                  value={proxy.username}
                  onChange={(e) => updateProxy({ username: e.target.value })}
                  disabled={proxy.type === "disabled"}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">{t("page.setting.net.proxy.password")}</Label>
                <Input
                  id="password"
                  type="password"
                  value={proxy.password}
                  onChange={(e) => updateProxy({ password: e.target.value })}
                  disabled={proxy.type === "disabled"}
                />
              </div>
            </div>
          )}

          <div className="flex justify-end">
            <Button className="mt-4" onClick={handleSave}>
              {t("page.setting.net.proxy.save")}
            </Button>
          </div>
        </CardContent>
      </Card>
    </main>
  );
}
