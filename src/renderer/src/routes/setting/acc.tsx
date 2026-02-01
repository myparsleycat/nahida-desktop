import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent } from "@renderer/components/ui/card";
import { useGlobalStore } from "@renderer/store/global";
import { createFileRoute, useNavigate } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/acc")({
  component: RouteComponent,
});

function RouteComponent() {
  const session = useGlobalStore((state) => state.session);
  const navi = useNavigate();

  if (!session) {
    return (
      <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none items-center justify-center h-full">
        <p className="text-muted-foreground mb-4">로그인이 필요합니다.</p>
        <Button onClick={() => window.api.invoke("auth:startLogin")}>로그인</Button>
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
              내 계정
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="flex flex-row items-center justify-end">
        <Button
          onClick={() => {
            window.api.invoke("auth:startLogout").then(() => {
              // navi({ to: "/auth" }); // No longer needed to redirect explicitly, or maybe redirect to home?
              // But logout updates session to null, which re-renders this component to show Login button.
            });
          }}
        >
          로그아웃
        </Button>
      </div>
    </main>
  );
}
