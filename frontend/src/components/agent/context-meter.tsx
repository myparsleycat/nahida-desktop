import type { AgentContextUsage } from "@bindings/agent/models";
import { Popover, PopoverContent, PopoverTrigger } from "@renderer/components/ui/popover";
import { contextOccupancy, formatTokens } from "@renderer/lib/agent-context";
import { cn } from "@renderer/lib/utils";
import { useTranslation } from "react-i18next";

/** Ring geometry: 14px viewBox, 2px stroke. */
const RADIUS = 5.5;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/** Legend rows in bar-segment order; each tint matches the segment it labels. */
const ROWS = [
  { key: "systemTokens", label: "page.agent.context_system", tint: "bg-muted-foreground/40" },
  { key: "toolsTokens", label: "page.agent.context_tools", tint: "bg-violet-400" },
  {
    key: "messageTokens",
    label: "page.agent.context_messages",
    tint: "bg-[var(--agent-blue,#4176e6)]",
  },
] as const;

/** Colors the single segment drawn when no composition is available. */
const FALLBACK_TINT = "bg-muted-foreground/40";

/**
 * Composer context-occupancy meter: a ring and percentage fed by a session's context usage, with a
 * click-open panel of its composition. Renders nothing until both the usage and a capacity are
 * known, so a model without a configured context window stays clean.
 */
export function AgentContextMeter({
  usage,
  className,
}: {
  usage?: AgentContextUsage | null;
  className?: string;
}) {
  const { t, i18n } = useTranslation();
  const context = contextOccupancy(usage);
  if (!usage || context === null) return null;

  const reading = `${context.percent}%`;
  // The bar's overall length stays the projected percent; the heuristic breakdown only proportions
  // its colored parts. A zero-width part is dropped rather than rendered, so an empty context never
  // paints a filled sliver over nothing.
  const breakdownTotal = usage.systemTokens + usage.toolsTokens + usage.messageTokens;
  const parts =
    breakdownTotal === 0
      ? [{ key: "total", tint: FALLBACK_TINT, width: context.percent }]
      : ROWS.map((row) => ({
          key: row.key,
          tint: row.tint,
          width: (context.percent * usage[row.key]) / breakdownTotal,
        }));
  const segments = parts.filter((part) => part.width > 0);

  return (
    <Popover>
      <PopoverTrigger
        aria-label={t("page.agent.context_used", { percent: reading })}
        className={cn(
          "flex h-[26px] flex-none items-center gap-1.5 rounded-full px-2 text-xs font-medium text-muted-foreground tabular-nums transition-colors duration-100 hover:bg-foreground/7 hover:text-foreground",
          className,
        )}
      >
        <svg viewBox="0 0 14 14" width="14" height="14" aria-hidden className="flex-none">
          <circle cx="7" cy="7" r={RADIUS} strokeWidth={2} className="fill-none stroke-border" />
          <circle
            cx="7"
            cy="7"
            r={RADIUS}
            strokeWidth={2}
            strokeLinecap="round"
            strokeDasharray={`${(CIRCUMFERENCE * context.percent) / 100} ${CIRCUMFERENCE}`}
            transform="rotate(-90 7 7)"
            className="fill-none stroke-(--agent-blue)"
          />
        </svg>
        <span>{reading}</span>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        aria-label={t("page.agent.context_used", { percent: reading })}
        className="w-64 gap-0 rounded-[12px] p-3 text-xs leading-5"
      >
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground">
            {t("page.agent.context_used", { percent: reading })}
          </span>
          <span className="ml-auto font-medium text-foreground tabular-nums">
            {`~${formatTokens(context.usedTokens, i18n.language)} / ${formatTokens(context.contextWindow, i18n.language)}`}
          </span>
        </div>
        <div className="my-2.5 flex h-1 gap-px overflow-hidden rounded-full bg-muted">
          {segments.map((segment) => (
            <div
              key={segment.key}
              style={{ width: `${segment.width}%` }}
              className={cn("h-full min-w-0.5 flex-none rounded-[1px]", segment.tint)}
            />
          ))}
        </div>
        <dl className="flex flex-col">
          {ROWS.map((row) => (
            <div key={row.key} className="flex items-center justify-between gap-3 py-0.5">
              <dt className="flex items-center gap-1.5 text-muted-foreground">
                <span className={cn("size-2 flex-none rounded-[2px]", row.tint)} aria-hidden />
                {t(row.label)}
              </dt>
              <dd className="text-foreground tabular-nums">
                {`~${formatTokens(usage[row.key], i18n.language)}`}
              </dd>
            </div>
          ))}
        </dl>
      </PopoverContent>
    </Popover>
  );
}
