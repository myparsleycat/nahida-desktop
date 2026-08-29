import { Shell } from "@bindings/platform";
import { Button } from "@renderer/components/ui/button";
import { Kbd } from "@renderer/components/ui/kbd";
import { cn } from "@renderer/lib/utils";
import type { ModIni, ToggleKey } from "@renderer/types/mod";
import { formatKeyLabel } from "@shared/key-formatter";
import { FileCogIcon, PlusIcon } from "lucide-react";
import { type ReactNode, memo, useMemo } from "react";

interface KeySettingRequest {
  id: string;
  label: string;
  sectionName: string;
  value: string;
  otherKeys: string[];
}

interface ModToggleKeyItemProps {
  toggleKey: ToggleKey;
  otherKeys: string[];
  onOpenKeySetting: (setting: KeySettingRequest) => void;
}

interface ModIniItemProps {
  ini: ModIni;
  otherKeysById: Record<string, string[]>;
  onOpenKeySetting: (setting: KeySettingRequest) => void;
}

function KeyDisplay({ keys }: { keys: string }) {
  const labels = useMemo(
    () =>
      keys
        .split(" ")
        .map((k) => formatKeyLabel(k))
        .filter((k): k is string => k !== null),
    [keys],
  );

  return (
    <div className="flex flex-wrap items-center gap-1">
      {labels.map((label, idx) => (
        <Kbd key={`${keys}:${idx.toString()}`} className="text-xs">
          {label}
        </Kbd>
      ))}
    </div>
  );
}

function KeySettingTrigger({
  label,
  value,
  onOpen,
  children,
}: {
  label: string;
  value: string;
  onOpen: () => void;
  children?: ReactNode;
}) {
  const trigger = children ?? (
    <button
      type="button"
      className={cn(
        "flex w-full flex-row items-center transition-colors",
        "space-x-1 rounded-lg border border-white/20 bg-foreground/5 p-2 hover:bg-background/10",
        "size-full flex-row justify-start p-1",
      )}
      onClick={(e) => {
        e.stopPropagation();
        onOpen();
      }}
    >
      <span className="text-sm">{label}:</span>
      <KeyDisplay keys={value} />
    </button>
  );

  if (children) {
    return (
      <div
        onClick={(e) => {
          e.stopPropagation();
          onOpen();
        }}
      >
        {children}
      </div>
    );
  }

  return trigger;
}

const ModToggleKeyItem = memo(function ModToggleKeyItem({
  toggleKey,
  otherKeys,
  onOpenKeySetting,
}: ModToggleKeyItemProps) {
  return (
    <div className="space-y-1 rounded-lg bg-foreground/5 p-1 shadow-sm">
      <div className="grid h-4.5 grid-cols-[1.5rem_1fr] p-0">
        <p className="flex h-5 w-5 items-center justify-center rounded border border-black/15 dark:border-white/15">
          {toggleKey.values.length}
        </p>
        <p className="truncate">{toggleKey.sectionName}</p>
      </div>

      {toggleKey.key ? (
        <KeySettingTrigger
          label="key"
          value={toggleKey.key}
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:key`,
              label: "key",
              sectionName: toggleKey.sectionName,
              value: toggleKey.key ?? "",
              otherKeys,
            })
          }
        />
      ) : (
        <KeySettingTrigger
          label="key"
          value=""
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:key`,
              label: "key",
              sectionName: toggleKey.sectionName,
              value: "",
              otherKeys,
            })
          }
        >
          <Button
            variant="outline"
            size="sm"
            className="w-full justify-start text-xs text-muted-foreground"
          >
            <PlusIcon className="mr-1.5 size-3" />
            Add key
          </Button>
        </KeySettingTrigger>
      )}

      {toggleKey.back ? (
        <KeySettingTrigger
          label="back"
          value={toggleKey.back}
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:back`,
              label: "back",
              sectionName: toggleKey.sectionName,
              value: toggleKey.back ?? "",
              otherKeys,
            })
          }
        />
      ) : (
        <KeySettingTrigger
          label="back"
          value=""
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:back`,
              label: "back",
              sectionName: toggleKey.sectionName,
              value: "",
              otherKeys,
            })
          }
        >
          <Button
            variant="outline"
            size="sm"
            className={cn(
              "flex w-full flex-row items-center transition-colors dark:text-muted-foreground",
              "space-x-1 rounded-lg border border-white/20 bg-foreground/5 p-2 hover:bg-background/10",
              "size-full flex-row justify-start p-1",
            )}
          >
            <PlusIcon className="mr-1.5 size-3" />
            Add back
          </Button>
        </KeySettingTrigger>
      )}
    </div>
  );
});

export const ModIniItem = memo(function ModIniItem({
  ini,
  otherKeysById,
  onOpenKeySetting,
}: ModIniItemProps) {
  return (
    <div className="space-y-1 text-[13px]">
      <div className="flex items-center justify-between gap-1">
        <p className="truncate whitespace-normal opacity-80" title={ini.name}>
          {ini.name}
        </p>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            void Shell.OpenPath(ini.path);
          }}
        >
          <FileCogIcon />
        </Button>
      </div>

      {ini.toggleKeys.length > 0 && (
        <div className="space-y-2.5">
          {ini.toggleKeys.map((toggleKey, idx) => (
            <ModToggleKeyItem
              key={`${ini.path}:${toggleKey.sectionName}:${idx.toString()}`}
              toggleKey={toggleKey}
              otherKeys={
                otherKeysById[`${ini.path}:${toggleKey.sectionName}:${idx.toString()}`] ?? []
              }
              onOpenKeySetting={onOpenKeySetting}
            />
          ))}
        </div>
      )}
    </div>
  );
});
