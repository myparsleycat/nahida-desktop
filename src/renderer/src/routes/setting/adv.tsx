import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@renderer/components/ui/popover";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { InfoIcon, Pencil, Save, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/setting/adv")({
  component: RouteComponent,
});

interface SettingItem {
  key: string;
  value: string | null;
}

function RouteComponent() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: settings, isLoading } = useQuery<SettingItem[]>({
    queryKey: ["settings", "advanced"],
    queryFn: async () => {
      return await window.api.invoke("setting:advanced:getAll");
    },
  });

  const updateMutation = useMutation({
    mutationFn: async ({ key, value }: { key: string; value: string }) => {
      await window.api.invoke("setting:advanced:set", key, value);
    },
    onSuccess: () => {
      toast.success("설정이 저장되었습니다.");
      queryClient.invalidateQueries({ queryKey: ["settings", "advanced"] });
    },
    onError: () => {
      toast.error("설정 저장에 실패했습니다.");
    },
  });

  if (isLoading) {
    return <div className="p-4">Loading...</div>;
  }

  return (
    <div className="space-y-4 p-3">
      <Alert variant="destructive">
        <InfoIcon />
        <AlertTitle>{t("page.setting.adv.warning_title")}</AlertTitle>
        <AlertDescription>{t("page.setting.adv.warning_description")}</AlertDescription>
      </Alert>

      <div className="border rounded-md overflow-hidden text-[13px]">
        <div className="grid grid-cols-[minmax(150px,1fr)_minmax(200px,3fr)_80px] bg-muted/50 border-b font-medium text-muted-foreground">
          <div className="p-3 uppercase tracking-wider">Key</div>
          <div className="p-3 uppercase tracking-wider">Value</div>
          <div className="p-3 uppercase tracking-wider text-center">Action</div>
        </div>
        <div className="grid grid-cols-[minmax(150px,1fr)_minmax(200px,3fr)_80px]">
          {settings?.map((setting) => (
            <SettingRow
              key={setting.key}
              setting={setting}
              onSave={(key, value) => updateMutation.mutate({ key, value })}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function SettingRow({
  setting,
  onSave,
}: {
  setting: SettingItem;
  onSave: (key: string, value: string) => void;
}) {
  const [isEditing, setIsEditing] = useState(false);
  const [value, setValue] = useState(setting.value || "");

  const handleSave = () => {
    onSave(setting.key, value);
    setIsEditing(false);
  };

  const handleCancel = () => {
    setValue(setting.value || "");
    setIsEditing(false);
  };

  return (
    <>
      <div className="p-3 border-b font-medium break-all flex items-center">{setting.key}</div>
      <div className="p-3 border-b min-w-0 flex items-center">
        {isEditing ? (
          <Input
            className="text-[13px]"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            autoFocus
          />
        ) : (
          <Popover>
            <PopoverTrigger
              render={
                <button
                  type="button"
                  className="truncate min-w-0 w-full cursor-pointer text-left hover:text-primary disabled:cursor-default"
                  disabled={setting.value == null}
                  title={setting.value ?? ""}
                />
              }
            >
              {setting.value}
            </PopoverTrigger>
            <PopoverContent
              align="start"
              className="max-h-[60vh] w-80 overflow-auto whitespace-pre-wrap break-all"
            >
              {setting.value}
            </PopoverContent>
          </Popover>
        )}
      </div>
      <div className="p-3 border-b flex items-center justify-center">
        {isEditing ? (
          <div className="flex items-center gap-1">
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={handleSave}>
              <Save className="h-4 w-4" />
            </Button>
            <Button size="icon" variant="ghost" className="h-8 w-8" onClick={handleCancel}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        ) : (
          <Button
            size="icon"
            variant="ghost"
            className="h-8 w-8"
            onClick={() => {
              setValue(setting.value || "");
              setIsEditing(true);
            }}
          >
            <Pencil className="h-4 w-4" />
          </Button>
        )}
      </div>
    </>
  );
}
