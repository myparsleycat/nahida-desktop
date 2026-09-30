import {
  CharacterSidebarContent,
  type CharacterSidebarContentProps,
} from "./character-sidebar-content";

export function CharacterSidebarGrid(props: CharacterSidebarContentProps) {
  return (
    <CharacterSidebarContent
      {...props}
      layout="grid"
      listClassName="grid gap-1.5 p-2"
      listStyle={{
        gridTemplateColumns:
          "repeat(auto-fill, minmax(min(120px, calc((100% - 0.375rem) / 2)), 1fr))",
      }}
      itemClassName="group rounded-lg bg-muted/40 hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      selectedItemClassName="bg-accent/50 ring-1 ring-primary"
      nestedItemClassName="border-l-4 border-l-primary/40 bg-muted/20"
    />
  );
}
