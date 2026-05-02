import type { CSSProperties } from "react";
import {
  CharacterSidebarContent,
  type CharacterSidebarContentProps,
} from "./character-sidebar-content";

const containerStyle: CSSProperties = {
  gridTemplateColumns: "auto 1fr auto",
};

export function CharacterSidebarRow(props: CharacterSidebarContentProps) {
  return (
    <CharacterSidebarContent
      {...props}
      layout="row"
      listClassName="flex flex-col"
      itemClassName="relative grid h-14 items-center gap-3 overflow-hidden py-2 pr-4 hover:bg-[#cecece] dark:hover:bg-[#2a2a2a]"
      selectedItemClassName="bg-[#cecece] dark:bg-[#2a2a2a]"
      itemStyle={(depth) => ({
        ...containerStyle,
        paddingLeft: depth > 0 ? `${depth * 16 + 8}px` : "8px",
      })}
    />
  );
}
