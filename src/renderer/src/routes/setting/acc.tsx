import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent } from "@renderer/components/ui/card";
import { createFileRoute, useNavigate } from "@tanstack/react-router";

export const Route = createFileRoute("/setting/acc")({
  component: RouteComponent,
  loader: async () => {
    const session = await window.api.invoke("auth:getSession");
    return { session };
  },
});

function RouteComponent() {
  const { session } = Route.useLoaderData();
  const navi = useNavigate();

  return (
    <main className="flex-1 flex flex-col mx-auto p-4 space-y-6 w-full select-none">
      <Card>
        <CardContent className="flex flex-row items-center justify-between">
          <div className="flex flex-row items-center space-x-3">
            <Avatar>
              <AvatarImage src={session?.user.image || undefined} />
              <AvatarFallback>{session?.user.name.charAt(0)}</AvatarFallback>
            </Avatar>

            <div className="flex flex-col text-sm">
              <p>{session?.user.name}</p>
              <p>{session?.user.email}</p>
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

      {/* <div className='space-y-1'>
        <p className='text-sm font-semibold ml-2'>저장 공간</p>
        <Card>
          <CardContent className='flex flex-row items-center justify-between'>
            <div className='flex flex-row items-center space-x-3'>
              <Avatar>
                <AvatarImage src={session.user.image || undefined} />
                <AvatarFallback>{session.user.name.charAt(0)}</AvatarFallback>
              </Avatar>

              <div className='flex flex-col text-sm'>
                <p>{session.user.name}</p>
                <p>{session.user.email}</p>
              </div>
            </div>

            <div>
              <Button
                size='sm'
                variant='secondary'
                onClick={() => window.api.invoke(
                  'util:openExternal',
                  `https://nahida.live/u`
                )}
              >
                내 계정
              </Button>
            </div>
          </CardContent>
        </Card>
      </div> */}

      <div className="flex flex-row items-center justify-end">
        <Button
          onClick={() => {
            window.api.invoke("auth:startLogout").then(() => {
              navi({ to: "/auth" });
            });
          }}
        >
          로그아웃
        </Button>
      </div>
    </main>
  );
}
