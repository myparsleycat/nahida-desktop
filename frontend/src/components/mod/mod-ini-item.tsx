import { Shell } from "@bindings/platform";
import { Button } from "@renderer/components/ui/button";
import { Kbd } from "@renderer/components/ui/kbd";
import { cn } from "@renderer/lib/utils";
import type { ModIni, ToggleKey } from "@renderer/types/mod";
import { formatKeyLabel } from "@shared/key-formatter";
import { toErrorMessage } from "@shared/utils";
import { FileCogIcon, PlusIcon } from "lucide-react";
import { memo, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface KeySettingRequest {
  id: string;
  label: string;
  sectionName: string;
  value: string;
}

interface ModToggleKeyItemProps {
  toggleKey: ToggleKey;
  onOpenKeySetting: (setting: KeySettingRequest) => void;
}

interface ModIniItemProps {
  ini: ModIni;
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
        <Kbd
          key={`${keys}:${idx.toString()}`}
          className="bg-black/6 text-xs text-foreground/75 dark:bg-muted dark:text-muted-foreground"
        >
          {label}
        </Kbd>
      ))}
    </div>
  );
}

function KeySettingTrigger({
  label,
  value,
  placeholder,
  onOpen,
}: {
  label: string;
  value: string;
  placeholder: string;
  onOpen: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex min-h-7.5 w-full flex-row items-center justify-start gap-1 rounded-lg border p-1 transition-colors",
        "border-black/5 bg-white/30 hover:bg-white/60",
        "dark:border-white/20 dark:bg-foreground/5 dark:hover:bg-foreground/10",
      )}
      onClick={(e) => {
        e.stopPropagation();
        onOpen();
      }}
    >
      {value ? (
        <>
          <span className="text-sm">{label}:</span>
          <KeyDisplay keys={value} />
        </>
      ) : (
        <span className="flex items-center gap-1.5 px-1.5 text-xs text-muted-foreground">
          <PlusIcon className="size-3" />
          {placeholder}
        </span>
      )}
    </button>
  );
}

const ModToggleKeyItem = memo(function ModToggleKeyItem({
  toggleKey,
  onOpenKeySetting,
}: ModToggleKeyItemProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-1 rounded-lg bg-white/35 p-1 shadow-sm dark:bg-foreground/5">
      <div className="grid h-4.5 grid-cols-[1.5rem_1fr] p-0">
        <p className="flex h-5 w-5 items-center justify-center rounded border border-black/15 dark:border-white/15">
          {toggleKey.values.length}
        </p>
        <p className="truncate">{toggleKey.sectionName}</p>
      </div>

      <KeySettingTrigger
        label="key"
        value={toggleKey.key ?? ""}
        placeholder={t("page.mod.ini.add_key")}
        onOpen={() =>
          onOpenKeySetting({
            id: `${toggleKey.sectionName}:key`,
            label: "key",
            sectionName: toggleKey.sectionName,
            value: toggleKey.key ?? "",
          })
        }
      />

      <KeySettingTrigger
        label="back"
        value={toggleKey.back ?? ""}
        placeholder={t("page.mod.ini.add_back_key")}
        onOpen={() =>
          onOpenKeySetting({
            id: `${toggleKey.sectionName}:back`,
            label: "back",
            sectionName: toggleKey.sectionName,
            value: toggleKey.back ?? "",
          })
        }
      />
    </div>
  );
});

export const ModIniItem = memo(function ModIniItem({ ini, onOpenKeySetting }: ModIniItemProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-1 text-[13px]">
      <div className="flex items-center justify-between gap-1">
        <p className="truncate whitespace-normal opacity-80" title={ini.name}>
          {ini.name}
        </p>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("page.mod.ini.open_file")}
          className="size-7 shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            void Shell.OpenPath(ini.path).catch((error: unknown) => {
              toast.error(toErrorMessage(error));
            });
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
              onOpenKeySetting={onOpenKeySetting}
            />
          ))}
        </div>
      )}
    </div>
  );
});
