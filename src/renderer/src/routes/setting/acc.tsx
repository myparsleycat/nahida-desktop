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
  const { session, sessionInitialized, startLogin, startLogout } = useAuth();
  const { t } = useTranslation();

  if (!sessionInitialized) {
    return (
      <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none items-center justify-center h-full">
        <Loader2Icon className="size-6 animate-spin text-muted-foreground" />
      </main>
    );
  }

  if (!session) {
    return (
      <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none items-center justify-center h-full">
        <p className="text-muted-foreground mb-4">
          {t("page.setting.acc.not_logged_in.description")}
        </p>
        <Button onClick={() => startLogin()}>
          {t("page.setting.acc.not_logged_in.login")}
        </Button>
      </main>
    );
  }

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
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
        <Button onClick={() => startLogout()}>
          {t("page.setting.acc.logout")}
        </Button>
      </div>
    </main>
  );
}
