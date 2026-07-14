import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent } from "@renderer/components/ui/card";
import { useAuth } from "@renderer/hooks/use-auth";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/acc")({
  component: RouteComponent,
});

function RouteComponent() {
  const { session, sessionInitialized, hasToken, isBackendOffline, startLogin, startLogout } =
    useAuth();
  const { t } = useTranslation();

  if (!sessionInitialized) {
    return (
      <main className="mx-auto flex h-full w-full flex-1 flex-col items-center justify-center space-y-6 p-4 select-none">
        <Loader2Icon className="size-6 animate-spin text-muted-foreground" />
      </main>
    );
  }

  if (isBackendOffline) {
    return (
      <main className="mx-auto flex h-full w-full flex-1 flex-col items-center justify-center space-y-6 p-4 select-none">
        <p className="mb-4 text-muted-foreground">{t("page.setting.acc.server_unreachable")}</p>
        {hasToken && <Button onClick={() => startLogout()}>{t("page.setting.acc.logout")}</Button>}
      </main>
    );
  }

  if (!session) {
    return (
      <main className="mx-auto flex h-full w-full flex-1 flex-col items-center justify-center space-y-6 p-4 select-none">
        <p className="mb-4 text-muted-foreground">
          {t("page.setting.acc.not_logged_in.description")}
        </p>
        <Button onClick={() => startLogin()}>{t("page.setting.acc.not_logged_in.login")}</Button>
      </main>
    );
  }

  return (
    <main className="mx-auto flex w-full flex-1 flex-col space-y-6 p-4 select-none">
      <Card>
        <CardContent className="flex flex-row items-center justify-between">
          <div className="flex flex-row items-center space-x-3">
            <Avatar>
              <AvatarImage src={session.user.image || undefined} />
              <AvatarFallback>{session.user.name.charAt(0)}</AvatarFallback>
            </Avatar>

            <div className="flex flex-col text-sm">
              <p>{session.user.name}</p>
              <p>{session.user.email}</p>
            </div>
          </div>

          <div>
            <Button
              variant="secondary"
              onClick={() => window.api.invoke("util:openExternal", `https://nahida.live/u`)}
            >
              {t("page.setting.acc.my_account")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="flex flex-row items-center justify-end">
        <Button onClick={() => startLogout()}>{t("page.setting.acc.logout")}</Button>
      </div>
    </main>
  );
}
