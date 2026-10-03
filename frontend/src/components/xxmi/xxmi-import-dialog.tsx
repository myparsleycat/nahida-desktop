import { XXMI } from "@bindings/xxmi";
import {
  ImportUserDataMode,
  ImportVersionMode,
  type ImportExternalLauncherInput,
} from "@bindings/xxmi/models";
import { Alert, AlertDescription } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@renderer/components/ui/table";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

export function XXMIImportDialog({
  path,
  root,
  onImport,
  onClose,
}: {
  path: string;
  root: string;
  onImport: (input: ImportExternalLauncherInput) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const preview = useQuery({
    queryKey: ["xxmi:import-preview", path],
    queryFn: () => XXMI.PreviewExternalLauncherImport(path),
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  const [selectedMode, setSelectedMode] = useState<ImportVersionMode | null>(null);
  const [userData, setUserData] = useState(ImportUserDataMode.ImportUserDataKeep);
  const [importing, setImporting] = useState(false);
  const versions = preview.data ?? [];
  const checkFailed = versions.some((version) => version.checkFailed);
  const hasUpdates = versions.some((version) => version.updateAvailable);
  const canPin = versions.every((version) => version.installedVersion !== "");
  const defaultMode =
    versions.length === 0 || checkFailed || hasUpdates
      ? ImportVersionMode.ImportVersionPinned
      : ImportVersionMode.ImportVersionLatest;
  const versionMode = checkFailed
    ? ImportVersionMode.ImportVersionPinned
    : (selectedMode ?? defaultMode);
  const canImport =
    !preview.isFetching &&
    !preview.isError &&
    (versionMode !== ImportVersionMode.ImportVersionPinned || canPin);

  const confirm = async () => {
    if (!canImport || importing) return;
    setImporting(true);
    await onImport({ path, root, userData, versionMode, versions }).finally(() =>
      setImporting(false),
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !importing && onClose()}>
      <DialogContent className="max-h-[85vh] sm:max-w-lg" showCloseButton={!importing}>
        <DialogHeader>
          <DialogTitle>{t("page.setting.xxmi.builtin.import")}</DialogTitle>
          <DialogDescription>
            {t("page.setting.xxmi.builtin.importUserDataDescription", { root })}
          </DialogDescription>
        </DialogHeader>
        {preview.isFetching ? (
          <p role="status" className="flex items-center gap-2 text-muted-foreground">
            <Loader2Icon className="size-4 animate-spin" />
            {t("page.setting.xxmi.builtin.importCheckingVersions")}
          </p>
        ) : preview.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{toErrorMessage(preview.error)}</AlertDescription>
          </Alert>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("page.setting.xxmi.builtin.importPackage")}</TableHead>
                  <TableHead>{t("page.setting.xxmi.builtin.importInstalledVersion")}</TableHead>
                  <TableHead>{t("page.setting.xxmi.builtin.importLatestVersion")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {versions.map((version) => (
                  <TableRow key={version.package}>
                    <TableCell>
                      {version.package === "xxmi-libs"
                        ? t("page.setting.xxmi.builtin.libs")
                        : version.package.replace("importer:", "")}
                    </TableCell>
                    <TableCell className="font-mono">
                      {version.installedVersion || t("page.setting.xxmi.builtin.notInstalled")}
                    </TableCell>
                    <TableCell className="font-mono">
                      {version.checkFailed ? t("g.unknown") : version.latestVersion}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {checkFailed && (
              <Alert>
                <AlertDescription>
                  {t("page.setting.xxmi.builtin.importVersionCheckFailed")}
                </AlertDescription>
              </Alert>
            )}
            {(hasUpdates || checkFailed) && (
              <fieldset disabled={importing} className="space-y-2">
                <legend className="mb-2 font-medium">
                  {t("page.setting.xxmi.builtin.importVersionChoice")}
                </legend>
                <label className="flex items-start gap-2">
                  <input
                    type="radio"
                    name="import-version-mode"
                    value={ImportVersionMode.ImportVersionLatest}
                    checked={versionMode === ImportVersionMode.ImportVersionLatest}
                    disabled={checkFailed}
                    onChange={() => setSelectedMode(ImportVersionMode.ImportVersionLatest)}
                    className="mt-1 accent-primary"
                  />
                  {t("page.setting.xxmi.builtin.importUpdateLatest")}
                </label>
                <label className="flex items-start gap-2">
                  <input
                    type="radio"
                    name="import-version-mode"
                    value={ImportVersionMode.ImportVersionPinned}
                    checked={versionMode === ImportVersionMode.ImportVersionPinned}
                    disabled={!canPin}
                    onChange={() => setSelectedMode(ImportVersionMode.ImportVersionPinned)}
                    className="mt-1 accent-primary"
                  />
                  {t("page.setting.xxmi.builtin.importPinInstalled")}
                </label>
              </fieldset>
            )}
            <p className="text-xs text-muted-foreground">
              {t("page.setting.xxmi.builtin.importVersionHint")}
            </p>
          </>
        )}
        {(preview.isError || checkFailed) && (
          <Button
            variant="outline"
            disabled={preview.isFetching || importing}
            onClickPromise={() => preview.refetch()}
          >
            {t("page.setting.xxmi.builtin.importRetryVersions")}
          </Button>
        )}
        <fieldset disabled={importing} className="space-y-3">
          <legend className="mb-2 font-medium">
            {t("page.setting.xxmi.builtin.importUserDataTitle")}
          </legend>
          {[ImportUserDataMode.ImportUserDataKeep, ImportUserDataMode.ImportUserDataMove].map(
            (mode) => (
              <label key={mode} className="flex items-start gap-2">
                <input
                  type="radio"
                  name="import-user-data"
                  value={mode}
                  checked={userData === mode}
                  onChange={() => setUserData(mode)}
                  className="mt-1 accent-primary"
                />
                <span>
                  <span className="font-medium">
                    {t(
                      `page.setting.xxmi.builtin.importUserData${mode === ImportUserDataMode.ImportUserDataKeep ? "Keep" : "Move"}`,
                    )}
                  </span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {t(
                      `page.setting.xxmi.builtin.importUserData${mode === ImportUserDataMode.ImportUserDataKeep ? "Keep" : "Move"}Hint`,
                    )}
                  </span>
                </span>
              </label>
            ),
          )}
        </fieldset>
        <DialogFooter>
          <Button variant="outline" disabled={importing} onClick={onClose}>
            {t("g.cancel")}
          </Button>
          <Button disabled={!canImport} onClickPromise={confirm}>
            {t("page.setting.xxmi.builtin.import")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
