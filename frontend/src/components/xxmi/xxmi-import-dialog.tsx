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
  Questionnaire,
  QuestionnaireChoice,
  QuestionnaireChoiceDescription,
  QuestionnaireChoices,
  QuestionnaireItem,
  QuestionnaireNext,
  QuestionnairePrevious,
  QuestionnaireProgress,
  QuestionnaireSubmit,
  QuestionnaireTitle,
} from "@renderer/components/ui/questionnaire";
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
  const ready = !preview.isFetching && !preview.isError;
  const canImport = ready && (versionMode !== ImportVersionMode.ImportVersionPinned || canPin);

  const confirm = async () => {
    if (!canImport || importing) return;
    setImporting(true);
    await onImport({ path, root, userData, versionMode, versions }).finally(() =>
      setImporting(false),
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !importing && onClose()}>
      <DialogContent
        className="flex max-h-[85vh] flex-col overflow-hidden sm:max-w-lg"
        showCloseButton={!importing}
      >
        <DialogHeader>
          <DialogTitle>{t("page.setting.xxmi.builtin.import")}</DialogTitle>
          <DialogDescription>
            {t("page.setting.xxmi.builtin.importUserDataDescription", { root })}
          </DialogDescription>
        </DialogHeader>
        <Questionnaire
          className="min-h-0 flex-1 gap-0"
          onSubmit={(event) => {
            event.preventDefault();
            void confirm();
          }}
        >
          <div className="-mx-4 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4 *:shrink-0">
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
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.xxmi.builtin.importVersionHint")}
                </p>
              </>
            )}
            {(preview.isError || checkFailed) && (
              <Button
                type="button"
                variant="outline"
                disabled={preview.isFetching || importing}
                onClickPromise={() => preview.refetch()}
              >
                {t("page.setting.xxmi.builtin.importRetryVersions")}
              </Button>
            )}

            {/* An item without an enabled answer blocks navigation, so the version step exists only when there is a choice to make. */}
            {ready && (hasUpdates || checkFailed) && (
              <QuestionnaireItem name="import-version-mode" required>
                <QuestionnaireTitle className="text-sm">
                  {t("page.setting.xxmi.builtin.importVersionChoice")}
                </QuestionnaireTitle>
                <QuestionnaireChoices>
                  <QuestionnaireChoice
                    value={ImportVersionMode.ImportVersionLatest}
                    checked={versionMode === ImportVersionMode.ImportVersionLatest}
                    disabled={importing || checkFailed}
                    onChange={() => setSelectedMode(ImportVersionMode.ImportVersionLatest)}
                  >
                    {t("page.setting.xxmi.builtin.importUpdateLatest")}
                  </QuestionnaireChoice>
                  <QuestionnaireChoice
                    value={ImportVersionMode.ImportVersionPinned}
                    checked={versionMode === ImportVersionMode.ImportVersionPinned}
                    disabled={importing || !canPin}
                    onChange={() => setSelectedMode(ImportVersionMode.ImportVersionPinned)}
                  >
                    {t("page.setting.xxmi.builtin.importPinInstalled")}
                  </QuestionnaireChoice>
                </QuestionnaireChoices>
              </QuestionnaireItem>
            )}
            {ready && (
              <QuestionnaireItem name="import-user-data" required>
                <QuestionnaireTitle className="text-sm">
                  {t("page.setting.xxmi.builtin.importUserDataTitle")}
                </QuestionnaireTitle>
                <QuestionnaireChoices>
                  {[
                    ImportUserDataMode.ImportUserDataKeep,
                    ImportUserDataMode.ImportUserDataMove,
                  ].map((mode) => (
                    <QuestionnaireChoice
                      key={mode}
                      value={mode}
                      checked={userData === mode}
                      disabled={importing}
                      onChange={() => setUserData(mode)}
                    >
                      {t(
                        `page.setting.xxmi.builtin.importUserData${mode === ImportUserDataMode.ImportUserDataKeep ? "Keep" : "Move"}`,
                      )}
                      <QuestionnaireChoiceDescription className="text-xs">
                        {t(
                          `page.setting.xxmi.builtin.importUserData${mode === ImportUserDataMode.ImportUserDataKeep ? "Keep" : "Move"}Hint`,
                        )}
                      </QuestionnaireChoiceDescription>
                    </QuestionnaireChoice>
                  ))}
                </QuestionnaireChoices>
              </QuestionnaireItem>
            )}
          </div>
          <DialogFooter className="sm:items-center">
            <QuestionnaireProgress
              className="sm:mr-auto"
              render={(props, state) =>
                state.total > 1 ? (
                  <div {...props} aria-valuetext={`${state.current} / ${state.total}`}>
                    {state.current} / {state.total}
                  </div>
                ) : null
              }
            />
            <Button type="button" variant="outline" disabled={importing} onClick={onClose}>
              {t("g.cancel")}
            </Button>
            {ready ? (
              <>
                <QuestionnairePrevious disabled={importing}>
                  {t("g.previous")}
                </QuestionnairePrevious>
                <QuestionnaireNext disabled={!canImport}>{t("g.next")}</QuestionnaireNext>
                <QuestionnaireSubmit disabled={!canImport || importing}>
                  {importing && <Loader2Icon className="size-4 animate-spin" aria-hidden="true" />}
                  {t("page.setting.xxmi.builtin.import")}
                </QuestionnaireSubmit>
              </>
            ) : (
              <Button type="button" disabled>
                {t("page.setting.xxmi.builtin.import")}
              </Button>
            )}
          </DialogFooter>
        </Questionnaire>
      </DialogContent>
    </Dialog>
  );
}
