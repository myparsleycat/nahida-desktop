import { Titlebar as BaseTitlebar } from "@renderer/components/titlebar";
import { useQuery } from "@tanstack/react-query";
import type { ComponentProps } from "react";
import { useMemo } from "react";

type TitlebarProps = ComponentProps<typeof BaseTitlebar>;

export function useTitlebar() {
  const { data: titlebarStyle } = useQuery({
    queryKey: ["settings", "general", "titlebarStyle"],
    queryFn: async () => window.api.invoke("setting:general:getTitlebarStyle"),
  });

  const Titlebar = useMemo(
    () =>
      function Titlebar(props: TitlebarProps) {
        if (titlebarStyle === "native") return null;
        return <BaseTitlebar {...props} />;
      },
    [titlebarStyle],
  );

  const screenHeight = titlebarStyle === "modern" ? `h-[calc(100vh-28px)]` : "h-screen";

  return {
    Titlebar,
    screenHeight,
    titlebarStyle,
  };
}
