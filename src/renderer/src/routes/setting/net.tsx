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
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/net")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();

  const [proxyType, setProxyType] = useState("disabled");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [requiresAuth, setRequiresAuth] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const [parent, _enableAnimations] = useAutoAnimate();

  useEffect(() => {
    window.api.invoke("setting:net:getProxy").then((settings) => {
      if (settings) {
        setProxyType(settings.type);
        setHost(settings.host || "");
        setPort(settings.port || "");
        setRequiresAuth(settings.requiresAuth || false);
        setUsername(settings.username || "");
        setPassword(settings.password || "");
      }
    });
  }, []);

  const handleSave = async () => {
    await window.api.invoke("setting:net:setProxy", {
      type: proxyType,
      host,
      port,
      requiresAuth,
      username: requiresAuth ? username : undefined,
      password: requiresAuth ? password : undefined,
    });
    toast.success(t("page.setting.net.proxy.saved"));
  };

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <Card className="w-full">
        <CardContent className="space-y-4" ref={parent}>
          <div className="space-y-2">
            <Label htmlFor="proxy-type">{t("page.setting.net.proxy.type")}</Label>
            <Select value={proxyType} onValueChange={setProxyType}>
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
                value={host}
                onChange={(e) => setHost(e.target.value)}
                disabled={proxyType === "disabled"}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="port">{t("page.setting.net.proxy.port")}</Label>
              <Input
                id="port"
                placeholder="8080"
                value={port}
                onChange={(e) => setPort(e.target.value)}
                disabled={proxyType === "disabled"}
              />
            </div>
          </div>

          <div className="flex items-center space-x-2 pt-2">
            <Checkbox
              id="requires-auth"
              checked={requiresAuth}
              onCheckedChange={(checked) => setRequiresAuth(checked === true)}
              disabled={proxyType === "disabled"}
            />
            <Label htmlFor="requires-auth" className="cursor-pointer font-normal">
              {t("page.setting.net.proxy.requiresAuth")}
            </Label>
          </div>

          {requiresAuth && (
            <div className="space-y-4 pt-2 border-t">
              <div className="space-y-2 pt-4">
                <Label htmlFor="username">{t("page.setting.net.proxy.username")}</Label>
                <Input
                  id="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  disabled={proxyType === "disabled"}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">{t("page.setting.net.proxy.password")}</Label>
                <Input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  disabled={proxyType === "disabled"}
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
