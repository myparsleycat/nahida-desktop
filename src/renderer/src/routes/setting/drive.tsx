import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Separator } from "@renderer/components/ui/separator";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import type { DriveNameSortPolicy } from "@shared/drive";
import { createFileRoute } from "@tanstack/react-router";
import { PlusIcon, XIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/drive")({
  component: RouteComponent,
});

const DRIVE_PASSWORD_LIST_MAX = 10;

const settingsConfig = {
  nameSortPolicy: "drive.nameSortPolicy",
  importPassword: "drive.importPassword",
  autoTryPasswords: "drive.autoTryPasswords",
  passwordList: "drive.passwordList",
} as const;

type PasswordRow = { id: string; value: string };

function createPasswordRow(value = ""): PasswordRow {
  return { id: crypto.randomUUID(), value };
}

function RouteComponent() {
  const { t } = useTranslation();
  const { settings, update, isLoading } = useSettings(settingsConfig);
  const [importPassword, setImportPassword] = useState("");

  useEffect(() => {
    if (!isLoading) setImportPassword(settings.importPassword);
  }, [isLoading, settings.importPassword]);

  if (isLoading) {
    return null;
  }

  return (
    <div className="space-y-6 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.drive.sorting.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-6">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.drive.nameSortPolicy.title")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.drive.nameSortPolicy.description")}
              </p>
            </div>
            <Select
              value={settings.nameSortPolicy}
              items={[
                {
                  value: "natural_ignore_spacing",
                  label: t("page.setting.drive.nameSortPolicy.options.natural_ignore_spacing"),
                },
                {
                  value: "natural",
                  label: t("page.setting.drive.nameSortPolicy.options.natural"),
                },
              ]}
              onValueChange={(value) => update("nameSortPolicy", value as DriveNameSortPolicy)}
            >
              <SelectTrigger className="w-52">
                <SelectValue placeholder={t("page.setting.drive.nameSortPolicy.select")} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="natural_ignore_spacing">
                    {t("page.setting.drive.nameSortPolicy.options.natural_ignore_spacing")}
                  </SelectItem>
                  <SelectItem value="natural">
                    {t("page.setting.drive.nameSortPolicy.options.natural")}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.drive.import.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-6">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.drive.autoTryPasswords.title")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.drive.autoTryPasswords.description")}
              </p>
            </div>
            <Switch
              checked={settings.autoTryPasswords}
              onCheckedChange={(val) => update("autoTryPasswords", val)}
            />
          </div>

          <Separator />

          <PasswordListSetting
            value={settings.passwordList}
            onChange={(value) => update("passwordList", value)}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.drive.importPassword.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          <p className="text-xs text-muted-foreground">
            {t("page.setting.drive.importPassword.description")}
          </p>
          <Input
            id="drive-import-password"
            type="password"
            autoComplete="new-password"
            value={importPassword}
            onChange={(event) => setImportPassword(event.target.value)}
            onBlur={() => void update("importPassword", importPassword)}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
            }}
            placeholder={t("page.setting.drive.importPassword.placeholder")}
          />
        </CardContent>
      </Card>
    </div>
  );
}

function PasswordListSetting({
  value,
  onChange,
}: {
  value: string[];
  onChange: (value: string[]) => void;
}) {
  const { t } = useTranslation();
  const [items, setItems] = useState<PasswordRow[]>(() => value.map(createPasswordRow));
  const itemsRef = useRef(items);
  const isFocusedRef = useRef(false);

  useEffect(() => {
    itemsRef.current = items;
  }, [items]);

  useEffect(() => {
    if (isFocusedRef.current) return;
    setItems((prev) => {
      if (
        prev.length === value.length &&
        prev.every((item, index) => item.value === value[index])
      ) {
        return prev;
      }
      return value.map((entry, index) =>
        prev[index]?.value === entry
          ? prev[index]
          : { id: prev[index]?.id ?? crypto.randomUUID(), value: entry },
      );
    });
  }, [value]);

  const commit = (next: PasswordRow[]) => {
    itemsRef.current = next;
    setItems(next);
    onChange(next.map((item) => item.value));
  };

  return (
    <div className="space-y-2">
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{t("page.setting.drive.passwordList.title")}</span>
        <p className="text-xs text-muted-foreground">
          {t("page.setting.drive.passwordList.description")}
        </p>
      </div>
      <div
        className="flex flex-col gap-1.5"
        onFocus={() => {
          isFocusedRef.current = true;
        }}
        onBlur={(event) => {
          if (
            event.relatedTarget instanceof Node &&
            event.currentTarget.contains(event.relatedTarget)
          ) {
            return;
          }
          isFocusedRef.current = false;
          onChange(itemsRef.current.map((row) => row.value));
        }}
      >
        {items.map((item) => (
          <div key={item.id} className="flex items-center gap-1.5">
            <Input
              value={item.value}
              placeholder={t("page.setting.drive.passwordList.placeholder")}
              className="h-8"
              onChange={(event) => {
                itemsRef.current = itemsRef.current.map((row) =>
                  row.id === item.id ? { ...row, value: event.target.value } : row,
                );
                setItems(itemsRef.current);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter") event.currentTarget.blur();
              }}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-7 shrink-0"
              onClick={() => commit(itemsRef.current.filter((row) => row.id !== item.id))}
            >
              <XIcon className="size-3.5" />
            </Button>
          </div>
        ))}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          disabled={items.length >= DRIVE_PASSWORD_LIST_MAX}
          onClick={() => {
            if (itemsRef.current.length >= DRIVE_PASSWORD_LIST_MAX) return;
            commit([...itemsRef.current, createPasswordRow()]);
          }}
        >
          <PlusIcon className="size-3.5" />
          {t("page.setting.drive.passwordList.add")}
        </Button>
      </div>
    </div>
  );
}
