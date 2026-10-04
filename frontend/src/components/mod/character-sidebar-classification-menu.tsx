import {
  ContextMenuRadioGroup,
  ContextMenuRadioItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@renderer/components/ui/context-menu";
import { useClassifications } from "@renderer/hooks/use-mod-data";
import { useClassificationMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { TagsIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

// Context-menu entries that assign a top-level character folder to one group per classification.
export function CharacterSidebarClassificationMenu({ group }: { group: FolderGroup }) {
  const { t } = useTranslation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const { data: classifications = [] } = useClassifications(selectedGame);
  const { setCharacterClassificationMutation } = useClassificationMutations();

  if (classifications.length === 0) {
    return null;
  }

  return (
    <>
      <ContextMenuSeparator />
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <TagsIcon className="h-4 w-4" />
          {t("page.mod.character-sidebar.classification.assign")}
        </ContextMenuSubTrigger>
        <ContextMenuSubContent>
          {classifications.map((classification) => {
            const assigned = group.classifications?.[classification.id];
            const assign = (groupId: string | null) =>
              setCharacterClassificationMutation.mutate({
                folderPath: group.path,
                classificationId: classification.id,
                groupId,
              });

            return (
              <ContextMenuSub key={classification.id}>
                <ContextMenuSubTrigger>{classification.name}</ContextMenuSubTrigger>
                <ContextMenuSubContent>
                  <ContextMenuRadioGroup
                    value={
                      classification.groups.some((item) => item.id === assigned) ? assigned : ""
                    }
                  >
                    <ContextMenuRadioItem value="" onClick={() => assign(null)}>
                      {t("page.mod.character-sidebar.classification.unassigned")}
                    </ContextMenuRadioItem>
                    {classification.groups.map((item) => (
                      <ContextMenuRadioItem
                        key={item.id}
                        value={item.id}
                        onClick={() => assign(item.id)}
                      >
                        {item.name}
                      </ContextMenuRadioItem>
                    ))}
                  </ContextMenuRadioGroup>
                </ContextMenuSubContent>
              </ContextMenuSub>
            );
          })}
        </ContextMenuSubContent>
      </ContextMenuSub>
    </>
  );
}
