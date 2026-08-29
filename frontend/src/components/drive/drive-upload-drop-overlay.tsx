import { useTranslation } from "react-i18next";

export function DriveUploadDropOverlay({
  visible,
  folderName,
}: {
  visible: boolean;
  folderName: string;
}) {
  const { t } = useTranslation();

  if (!visible) return null;

  return (
    <div className="absolute inset-0 z-50 flex h-full flex-1 items-center justify-center border-2 border-dashed border-primary bg-background/80 backdrop-blur-sm">
      <div className="text-center">
        <p className="text-2xl font-bold">
          {t("page.drive.dad_section.title", { name: folderName })}
        </p>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("page.drive.dad_section.description")}
        </p>
      </div>
    </div>
  );
}
