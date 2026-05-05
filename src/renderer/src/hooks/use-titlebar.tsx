import { Titlebar as BaseTitlebar } from "@renderer/components/titlebar";
import { useSetting } from "@renderer/hooks/use-settings";
import type { ComponentProps } from "react";
import { useMemo } from "react";

type TitlebarProps = ComponentProps<typeof BaseTitlebar>;

export function useTitlebar() {
  const { data: titlebarStyle } = useSetting("general.titlebarStyle");

  const Titlebar = useMemo(
    () =>
      function Titlebar(props: TitlebarProps) {
        if (titlebarStyle === "native") return null;
        return <BaseTitlebar {...props} />;
      },
    [titlebarStyle],
  );

  const screenHeight = titlebarStyle === "modern" ? `h-[calc(100vh-32px)]` : "h-screen";

  return {
    Titlebar,
    screenHeight,
    titlebarStyle,
  };
}
