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
import { useSettings } from "@renderer/hooks/use-settings";
import type { DriveNameSortPolicy } from "@shared/drive";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/drive")({
  component: RouteComponent,
});

const settingsConfig = {
  nameSortPolicy: "drive.nameSortPolicy",
  importPassword: "drive.importPassword",
} as const;

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

  const nameSortPolicyLabels: Record<string, string> = {
    natural_ignore_spacing: t("page.setting.drive.nameSortPolicy.options.natural_ignore_spacing"),
    natural: t("page.setting.drive.nameSortPolicy.options.natural"),
  };

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
              onValueChange={(value) => update("nameSortPolicy", value as DriveNameSortPolicy)}
            >
              <SelectTrigger className="w-52">
                <SelectValue placeholder={t("page.setting.drive.nameSortPolicy.select")}>
                  {(value) => nameSortPolicyLabels[value] ?? value}
                </SelectValue>
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
