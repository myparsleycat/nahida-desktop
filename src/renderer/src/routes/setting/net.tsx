import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/net")({
  component: RouteComponent,
});

function RouteComponent() {
  const [proxyType, setProxyType] = useState("disabled");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [requiresAuth, setRequiresAuth] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const [parent, _enableAnimations] = useAutoAnimate();

  useEffect(() => {
    window.electron.ipcRenderer.invoke("setting:net:getProxy").then((settings) => {
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
    await window.electron.ipcRenderer.invoke("setting:net:setProxy", {
      type: proxyType,
      host,
      port,
      requiresAuth,
      username: requiresAuth ? username : undefined,
      password: requiresAuth ? password : undefined,
    });
    toast.success("프록시 설정이 저장되었습니다.");
  };

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <Card className="w-full">
        <CardContent className="space-y-4" ref={parent}>
          <div className="space-y-2">
            <Label htmlFor="proxy-type">프록시 유형</Label>
            <Select value={proxyType} onValueChange={setProxyType}>
              <SelectTrigger id="proxy-type">
                <SelectValue placeholder="Select proxy type" />
              </SelectTrigger>
              <SelectContent position="popper">
                <SelectItem value="disabled">사용 안 함</SelectItem>
                <SelectItem value="https">HTTPS</SelectItem>
                <SelectItem value="socks5">SOCKS5</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2 space-y-2">
              <Label htmlFor="host">호스트</Label>
              <Input
                id="host"
                placeholder="proxy.example.com"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                disabled={proxyType === "disabled"}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="port">포트</Label>
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
              프록시 서버가 암호를 요구함
            </Label>
          </div>

          {requiresAuth && (
            <div className="space-y-4 pt-2 border-t">
              <div className="space-y-2 pt-4">
                <Label htmlFor="username">사용자 이름</Label>
                <Input
                  id="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  disabled={proxyType === "disabled"}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">암호</Label>
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
              저장
            </Button>
          </div>
        </CardContent>
      </Card>
    </main>
  );
}
