import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { DropletsIcon, HandIcon, PersonStandingIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

const APPLIED_TOOLS = [
  { id: "body-shape", Icon: PersonStandingIcon, label: "page.mod.applied-tools.body-shape" },
  { id: "touch-profile", Icon: HandIcon, label: "page.mod.applied-tools.touch-profile" },
  { id: "liquid-emitter", Icon: DropletsIcon, label: "page.mod.applied-tools.liquid-emitter" },
] as const;

export function ModAppliedToolIcons({ tools }: { tools?: string[] }) {
  const { t } = useTranslation();

  return APPLIED_TOOLS.filter((tool) => tools?.includes(tool.id)).map((tool) => (
    <Tooltip key={tool.id} disableHoverablePopup>
      <TooltipTrigger render={<span />} className="shrink-0 text-muted-foreground">
        <tool.Icon className="size-4" aria-label={t(tool.label)} />
      </TooltipTrigger>
      <TooltipContent>{t(tool.label)}</TooltipContent>
    </Tooltip>
  ));
}
