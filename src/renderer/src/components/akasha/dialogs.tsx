import { VisuallyHidden } from "@radix-ui/react-visually-hidden";
import { Center, Random1619 } from "@renderer/components/common";
import { DatePicker } from "@renderer/components/date-picker";
import { DiscordIcon } from "@renderer/components/icon";
import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@renderer/components/ui/avatar";
import { Button, buttonVariants } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { cn } from "@renderer/lib/utils";
import { useDialogStore, useSelectionStore } from "@renderer/store/drive";
import { Content } from "@shared/types.gen";
import { useForm, type AnyFieldApi } from "@tanstack/react-form";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useLocation, useParams, useRouteContext } from "@tanstack/react-router";
import { format } from "date-fns";
import { t } from "i18next";
import {
  AlertTriangleIcon,
  CopyIcon,
  EarthIcon,
  LinkIcon,
  Loader2Icon,
  LoaderIcon,
  LockIcon,
  SaveIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const ValidateName = (name: string) => {
  if (!name.trim()) {
    return t("#.ValidateName.0");
  } else if (name[0] === " ") {
    return t("#.ValidateName.1");
  } else if (name[name.length - 1] === " ") {
    return t("#.ValidateName.2");
  } else if (name.length <= 0 || name.length > 255) {
    return t("#.ValidateName.3");
  }

  return null;
};

export function RenameDialog() {
  const { t } = useTranslation();
  const dialog = useDialogStore();
  const selection = useSelectionStore();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();

  const id = location.pathname.split("/").pop() || "";

  const mutation = useMutation({
    mutationKey: ["drive", "rename", id],
    mutationFn: async ({ item, rename }: { item: Content; rename: string }) => {
      const data = await window.api.invoke("drive:patch:rename", item.id, rename);
      return data;
    },
  });

  if (selection.selectedItems[0]) {
    return (
      <Dialog
        open={dialog.renameDialog.open}
        onOpenChange={(v) => dialog.setOpen("renameDialog", v)}
      >
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t("page.drive.dialog.rename.title")}</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col space-y-4"
            autoComplete="off"
            onSubmit={async (e) => {
              e.preventDefault();

              const form = e.target as HTMLFormElement;
              const formData = new FormData(form);

              const name = formData.get("name") as string;
              if (!name || typeof name !== "string" || name.trim() === "") {
                toast.warning(t("page.drive.dialog.rename.#.rename.0"));
                return;
              }

              const ext = (formData.get("ext") as string) || "";

              const rename = name + ext;

              const validate_result = ValidateName(rename);
              if (validate_result) {
                return toast.warning(t("page.drive.dialog.rename.#.rename.1"), {
                  description: validate_result,
                });
              }

              const renamePromise = mutation.mutateAsync({
                item: selection.selectedItems[0],
                rename,
              });

              toast.promise(renamePromise, {
                loading: t("page.drive.dialog.rename.#.toast-promise.loading"),
                success: async () => {
                  await queryClient.invalidateQueries();
                  dialog.setOpen("renameDialog", false);
                  return t("page.drive.dialog.rename.#.toast-promise.success");
                },
                error: (e: any) => e.message,
              });
            }}
          >
            <div className="flex flex-row gap-x-4">
              <input
                className={cn(
                  "block w-full rounded-lg border-none bg-black/5 dark:bg-white/5 py-2 px-3 text-sm/6 text-black dark:text-white",
                  "focus:outline-2 focus:-outline-offset-2 focus:outline-black/25 focus:dark:outline-white/25",
                )}
                name="name"
                placeholder={t("page.drive.dialog.rename.name_input_placeholder")}
                maxLength={200}
                required
                defaultValue={
                  selection.selectedItems.length === 1 &&
                  !selection.selectedItems[0].isDir &&
                  selection.selectedItems[0].name.includes(".")
                    ? selection.selectedItems[0].name.split(".").slice(0, -1).join(".")
                    : selection.selectedItems.length === 1
                      ? selection.selectedItems[0].name
                      : ""
                }
              />
              {!selection.selectedItems[0].isDir && (
                <input
                  className={cn(
                    "block w-1/4 rounded-lg border-none bg-black/5 dark:bg-white/5 py-2 px-3 text-sm/6 text-black dark:text-white",
                    "focus:outline-2 focus:-outline-offset-2 focus:outline-black/25 focus:dark:outline-white/25",
                  )}
                  name="ext"
                  placeholder={t("page.drive.dialog.rename.extension")}
                  maxLength={50}
                  defaultValue={
                    selection.selectedItems.length === 1 &&
                    selection.selectedItems[0].name.includes(".")
                      ? "." + selection.selectedItems[0].name.split(".").pop()
                      : ""
                  }
                />
              )}
            </div>

            <div className="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2">
              <Button
                type="button"
                variant="outline"
                onClick={async (e) => {
                  e.preventDefault();
                  dialog.setOpen("renameDialog", false, undefined);
                }}
              >
                {t("g.cancel")}
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {t("g.confirm")}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    );
  } else {
    return null;
  }
}

export function NewDirectoryDialog({ contents }: { contents: Content[] }) {
  const { t } = useTranslation();
  const dialog = useDialogStore();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();

  const id = location.pathname.split("/").pop() || "";

  const mutation = useMutation({
    mutationKey: ["akasha", "make_dir", id],
    mutationFn: async (name: string) => {
      await window.api.invoke("drive:post:dir", id, name);
    },
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    const form = e.target as HTMLFormElement;
    const formData = new FormData(form);
    const name = formData.get("name") as string;

    const validate_result = ValidateName(name);
    if (validate_result) {
      return toast.warning(t("page.drive.dialog.create_dir.#.0"), {
        description: validate_result,
      });
    }

    if (contents.some((item) => item.isDir && item.name === name)) {
      return toast.warning(t("page.drive.dialog.create_dir.#.2"));
    }

    await mutation
      .mutateAsync(name)
      .then(async () => {
        toast.success(t("page.drive.dialog.create_dir.#.toast-promise.success"));
        dialog.setOpen("createDirDialog", false);
        await queryClient.invalidateQueries();
      })
      .catch((err) => {
        toast.error(err.message);
      });
  };

  return (
    <Dialog
      open={dialog.createDirDialog.open}
      onOpenChange={(v) => dialog.setOpen("createDirDialog", v)}
    >
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t("page.drive.dialog.create_dir.title")}</DialogTitle>
        </DialogHeader>
        <form className="flex flex-col space-y-4" autoComplete="off" onSubmit={handleSubmit}>
          <Input
            name="name"
            placeholder={t("page.drive.dialog.create_dir.name_input_placeholder")}
            maxLength={255}
            required
          />
          <div className="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2">
            <Button
              type="button"
              variant="outline"
              onClick={(e) => {
                e.preventDefault();
                dialog.setOpen("createDirDialog", false);
              }}
            >
              {t("g.cancel")}
            </Button>
            <Button type="submit" className="flex items-center gap-2">
              {mutation.isPending && <Loader2Icon />}
              {t("g.confirm")}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function PubLinkDialog() {
  const dialog = useDialogStore();
  const { t } = useTranslation();

  const itemId = useMemo(() => {
    return dialog.shareDialog.data?.id as string | undefined;
  }, [dialog.shareDialog.data]);

  const [password, setPassword] = useState("");
  const [selectedDate, setSelectedDate] = useState<Date | undefined>(undefined);
  const [pubLinkSwitchChecked, setPubLinkSwitch] = useState(false);

  const query = useQuery({
    queryKey: ["akasha", "drive", "publink", itemId],
    queryFn: async () => {
      if (!itemId) {
        throw new Error("ID를 가져오는데 실패함");
      }

      const { data, error } = await eden.akasha.content.share({ id: itemId }).get();

      if (error) {
        throw new Error(error.value.toString());
      }

      return data;
    },
    enabled: !!itemId,
  });

  useEffect(() => {
    if (query.data) {
      if (query.data.link?.expires_at) {
        setSelectedDate(new Date(query.data.link.expires_at));
      }

      setPubLinkSwitch(!!query.data.link);
    }
  }, [query.data]);

  const IDISEMPTY = "아이템 ID를 가져오는데 실패함";

  const changePermissionMutation = useMutation({
    mutationKey: ["akasha", "drive", "pub-link-dialog", "permission", "change"],
    mutationFn: async (pid: string) => {
      if (!query.data?.id) {
        throw new Error(IDISEMPTY);
      }

      const { data, error } = await eden.akasha.content.share
        .permission({ id: query.data.id })
        .p({ pid })
        .patch();

      if (error) {
        throw new Error(error.value.toString());
      }

      return data.r;
    },
  });

  const handleChangePermissionBtn = async (pid: string) => {
    return changePermissionMutation
      .mutateAsync(pid)
      .then(() => {
        toast.success("권한이 변경되었습니다");
        query.refetch();
      })
      .catch((err) => {
        toast.error("권한 변경 중 오류 발생", {
          description: err.message,
        });
      });
  };

  const deletePermissionMutation = useMutation({
    mutationKey: ["akasha", "drive", "pub-link-dialog", "permission", "delete"],
    mutationFn: async (pid: string) => {
      if (!query.data?.id) {
        throw new Error(IDISEMPTY);
      }

      const { data, error } = await eden.akasha.content.share
        .permission({ id: query.data.id })
        .p({ pid })
        .delete();

      if (error) {
        throw new Error(error.value.toString());
      }

      return true;
    },
  });

  const handleDeletePermissionBtn = async (pid: string) => {
    return deletePermissionMutation
      .mutateAsync(pid)
      .then(() => {
        toast.success("권한이 제거되었습니다");
        query.refetch();
      })
      .catch((err) => {
        toast.error("권한 삭제 중 오류 발생", {
          description: err.message,
        });
      });
  };

  const handleInviteUrlBtn = async () => {
    if (!query.data?.id) {
      toast.error(IDISEMPTY);
      return;
    }

    const { data, error } = await eden.akasha.content.share
      .permission({ id: query.data.id })
      .invite_url.get();

    if (error) {
      return toast.warning("초대 URL 가져오는중 오류 발생", {
        description: error.value.toString(),
      });
    }

    copyStr(data.url);
  };

  const pwdMutation = useMutation({
    mutationKey: ["akasha", "drive", "pub-link-dialog", "link", "password"],
    mutationFn: async ({ id, bool, password }: { id: string; bool: boolean; password: string }) => {
      const { data, error } = await eden.akasha.content.share.link({ id }).password.patch({
        value: bool ? null : password,
      });

      if (error) {
        throw new Error(error.value.toString());
      }

      return data;
    },
  });

  const handlePwdBtn = async (e: React.FormEvent, bool: boolean) => {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const formData = new FormData(form);
    const password = formData.get("password")?.toString()!;

    if (!query.data?.link) {
      return toast.warning(IDISEMPTY);
    }

    await pwdMutation.mutateAsync({
      id: query.data.link.id,
      bool,
      password,
    });

    toast.success(bool ? "비밀번호가 제거되었습니다" : "비밀번호가 생성되었습니다");

    await query.refetch();
  };

  const linkExpiresPatchMutation = useMutation({
    mutationKey: ["akasha", "drive", "pub-link-dialog", "link", "expires"],
    mutationFn: async ({ id, value }: { id: string; value: string | undefined }) => {
      const { data, error } = await eden.akasha.content.share.link({ id }).expires.patch({
        value: value || null,
      });

      if (error) {
        throw new Error(error.value.toString());
      }

      return data;
    },
  });

  const handleDatePickerSave = async () => {
    if (!query.data?.id) {
      return toast.warning(IDISEMPTY);
    }

    let value: string = "";
    if (selectedDate) {
      value = format(selectedDate, "yyyy-MM-dd");
    }

    await linkExpiresPatchMutation.mutateAsync({
      id: query.data.link?.id!,
      value,
    });

    toast.success(value ? "만료일이 설정되었습니다" : "만료일이 해제되었습니다");

    await query.refetch();
  };

  const handlePubLinkSwitch = async () => {
    if (!query.data?.link) {
      const { data, error } = await eden.akasha.content.share.link.post(null, {
        query: { item_id: itemId! },
      });

      if (error) {
        setPubLinkSwitch(false);
        return toast.error(error.value.toString());
      }
    } else {
      const { data, error } = await eden.akasha.content.share
        .link({ id: query.data?.link?.id! })
        .delete();

      if (error) {
        setPubLinkSwitch(true);
        return toast.warning(error.value.toString());
      }
    }

    toast.success(!query.data?.link ? "공개 링크가 생성되었습니다" : "공개 링크가 제거되었습니다");

    await query.refetch();
  };

  return (
    <Dialog
      open={dialog.shareDialog.open}
      onOpenChange={(v) => {
        if (!v) setSelectedDate(undefined);
        dialog.setOpen("shareDialog", v);
      }}
    >
      <DialogTitle></DialogTitle>
      <DialogContent aria-describedby={undefined}>
        {query.data ? (
          <div className="flex flex-col gap-y-8 w-full">
            <div className="w-full">
              <Label>액세스 권한이 있는 사용자</Label>
              <div className="flex flex-row mt-2 items-center space-x-4">
                {query.data.permissions.length > 0 ? (
                  <div className="grid grid-cols-8 gap-4 select-none max-h-28 overflow-y-auto overflow-x-hidden p-1">
                    {query.data.permissions.map((permission) => (
                      <div className="flex">
                        <DropdownMenu>
                          <DropdownMenuTrigger>
                            <Avatar>
                              <AvatarImage
                                src={permission.image || "https://placehold.co/100"}
                                alt={permission.name}
                              />
                              <AvatarFallback>{permission.name}</AvatarFallback>
                            </Avatar>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent>
                            <DropdownMenuGroup>
                              <DropdownMenuLabel>
                                {permission.name} (
                                {permission.permission === "VIEW"
                                  ? "조회 가능"
                                  : permission.permission === "EDIT"
                                    ? "편집 가능"
                                    : permission.permission === "UPLOAD"
                                      ? "업로드 가능"
                                      : permission.permission}
                                )
                              </DropdownMenuLabel>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                className="cursor-pointer"
                                onClick={async () => handleChangePermissionBtn(permission.id)}
                              >
                                권한 변경
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                className="cursor-pointer"
                                onClick={async () => handleDeletePermissionBtn(permission.id)}
                              >
                                권한 삭제
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    ))}
                  </div>
                ) : (
                  <Alert>
                    <AlertTriangleIcon className="h-4 w-4" />
                    <AlertTitle>항목이 비어있음</AlertTitle>
                    <AlertDescription>액세스 권한이 부여된 사용자가 없습니다</AlertDescription>
                  </Alert>
                )}

                <div>
                  <Tooltip delayDuration={50}>
                    <TooltipTrigger
                      className={buttonVariants({
                        variant: "outline",
                        size: "icon",
                      })}
                      onClick={handleInviteUrlBtn}
                    >
                      <LinkIcon className="pointer-events-none" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-sm w-full">
                      <p>
                        이 항목에 접근 가능한 권한을 부여하는 초대 링크를 복사합니다. 링크는 복사된
                        시점부터 24시간 동안 사용 가능합니다
                      </p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
            </div>

            <div className="w-full">
              <Label>일반 액세스</Label>
              <div className="flex flex-col mt-2">
                <div className="flex flex-row items-center gap-4 w-full">
                  <div className="flex">
                    {query.data.link ? <EarthIcon color="green" /> : <LockIcon color="orange" />}
                  </div>
                  <div>
                    <p className="text-base">{query.data.link ? <>공유중</> : <>제한됨</>}</p>
                    <p className="text-sm">
                      {query.data.link ? (
                        <>링크가 있는 모든 사용자가 볼 수 있음</>
                      ) : (
                        <>액세스 권한이 있는 사용자만 볼 수 있음</>
                      )}
                    </p>
                  </div>
                  <div className="flex justify-end grow">
                    <Switch
                      checked={pubLinkSwitchChecked}
                      onCheckedChange={setPubLinkSwitch}
                      onClick={handlePubLinkSwitch}
                    />
                  </div>
                </div>

                {query.data.link && (
                  <div className="flex flex-col gap-4">
                    <div className="flex mt-4 space-x-4">
                      <div className="flex w-full max-w-sm flex-col gap-1.5">
                        <Label>{t("g.password")}</Label>
                        <form
                          className="relative"
                          onSubmit={(e) => {
                            handlePwdBtn(e, !!query.data.link?.password);
                          }}
                        >
                          <Input
                            type="text"
                            name="password"
                            // defaultValue={query.data.link.password ? "********" : ""}
                            disabled={query.data.link.password}
                            required
                            minLength={4}
                            maxLength={150}
                            onValueChange={setPassword}
                            value={password}
                          />
                          <div className="absolute inset-y-0 right-2 flex items-center">
                            <button type="submit">
                              {query.data.link.password ? <XIcon /> : <SaveIcon />}
                            </button>
                          </div>
                        </form>
                      </div>

                      <div className="flex w-full max-w-sm flex-col gap-1.5">
                        <Label>공유 만료일</Label>
                        <div className="relative">
                          <DatePicker
                            value={selectedDate}
                            onChange={setSelectedDate}
                            disabled={(date) => {
                              const today = new Date();
                              today.setHours(0, 0, 0, 0);

                              return date <= today;
                            }}
                          />
                          <div className="absolute inset-y-0 right-2 flex items-center">
                            <button
                              className="pointer-events-auto z-50"
                              onClick={handleDatePickerSave}
                            >
                              <SaveIcon />
                            </button>
                          </div>
                        </div>
                      </div>
                    </div>

                    <div className="flex flex-row gap-x-3">
                      <input
                        className={cn(
                          "w-full rounded-lg border-none bg-black/5 dark:bg-white/5 py-1.5 px-3 text-sm/6",
                          "focus:outline-hidden data-focus:outline-2 data-focus:-outline-offset-2 data-focus:outline-white/25",
                        )}
                        type="link"
                        id="link"
                        value={query.data.link.url}
                        readOnly
                      />
                      <Button
                        type="button"
                        className="aspect-square"
                        variant="outline"
                        size="icon"
                        onClick={() => {
                          const str = query.data.link?.url;

                          if (!str) {
                            toast.warning("Cannot found Link URL");
                            return;
                          }

                          copyStr(str);
                        }}
                      >
                        <CopyIcon className="pointer-events-none" />
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        ) : (
          <div className="flex justify-center items-center">
            <LoaderIcon className="animate-spin-1.5" size={40} />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export function EmptyTrashDialog() {
  const { t } = useTranslation();
  const { emptyTrashDialog, setOpen } = useDialogStore();
  const { queryClient } = useRouteContext({ from: "__root__" });

  return (
    <AlertDialog open={emptyTrashDialog.open} onOpenChange={(v) => setOpen("emptyTrashDialog", v)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("drive.ui.empty_trash")}</AlertDialogTitle>
          <AlertDialogDescription>{t("drive.ui.empty_trash_dialog.0")}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              akasha.empty().then(() => {
                queryClient.refetchQueries({
                  queryKey: ["akasha:drive:trash"],
                });
                setOpen("emptyTrashDialog", false);
              });
            }}
          >
            {t("drive.ui.empty_trash_dialog.1")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function ConflictNameDialog() {
  const { t } = useTranslation();
  const { conflictNameDialog, setOpen, resolveDialog } = useDialogStore();
  const conflicts = conflictNameDialog.data?.conflicts ?? [];
  const preview = conflicts.slice(0, 6);
  const hiddenCount = Math.max(conflicts.length - preview.length, 0);

  return (
    <AlertDialog
      open={conflictNameDialog.open}
      onOpenChange={(open) => {
        setOpen("conflictNameDialog", open);
        if (!open) {
          resolveDialog("conflictNameDialog", "cancel");
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("page.drive.dialog.conflict_name.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("page.drive.dialog.conflict_name.description", { count: conflicts.length })}
            <br />
            {t("page.drive.dialog.conflict_name.option_suffix")}
            <br />
            {t("page.drive.dialog.conflict_name.option_skip")}
          </AlertDialogDescription>
          {preview.length > 0 && (
            <div className="w-full text-left mt-2 text-xs text-muted-foreground">
              <p>{t("page.drive.dialog.conflict_name.preview_label")}</p>
              <p>{preview.join(", ")}</p>
              {hiddenCount > 0 && (
                <p>{t("page.drive.dialog.conflict_name.preview_more", { count: hiddenCount })}</p>
              )}
            </div>
          )}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "skip");
            }}
          >
            {t("page.drive.dialog.conflict_name.action_skip")}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "suffix");
            }}
          >
            {t("page.drive.dialog.conflict_name.action_suffix")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function NotiDialog() {
  const { t } = useTranslation();
  const { notiDialog, setOpen } = useDialogStore();

  const query = useQuery({
    queryKey: ["akasha", "notiDialog", notiDialog.data?.id],
    queryFn: async () => {
      const { data, error } = await eden.akasha.webhook
        .item({ itemId: notiDialog.data.id as string })
        .get();

      if (error) {
        if (error.status === 404) {
          return null;
        }

        throw new Error(error.value.toString());
      }

      return data;
    },
    enabled: !!notiDialog.data?.id,
  });

  const form = useForm({
    defaultValues: {
      name: "",
      provider: "discord",
      url: "",
    },
    onSubmit: async ({ value }) => {
      const { name, provider, url } = value;

      const link = notiDialog.data?.link;
      const srcId = notiDialog.data?.id;
      if (!srcId) {
        throw new Error("selecton item is empty");
      }

      const { data, error } = await eden.akasha.webhook.post({
        name,
        srcId,
        // @ts-ignore
        provider,
        webhookUrl: url,
        ...(link && { link }),
      });

      if (error) {
        throw new Error(error.value.toString());
      }

      await query.refetch();

      return data;
    },
  });

  useEffect(() => {
    if (notiDialog.open) {
      form.reset();
    }
  }, [notiDialog.open, form]);

  function FieldInfo({ field }: { field: AnyFieldApi }) {
    return (
      <>
        {field.state.meta.isTouched && !field.state.meta.isValid ? (
          <em className="text-destructive">{field.state.meta.errors.join(", ")}</em>
        ) : null}
        {field.state.meta.isValidating ? "Validating..." : null}
      </>
    );
  }

  return (
    <Dialog open={notiDialog.open} onOpenChange={(v) => setOpen("notiDialog", v)}>
      <DialogContent aria-describedby={undefined} showCloseButton={false}>
        <VisuallyHidden>
          <DialogTitle></DialogTitle>
        </VisuallyHidden>

        {query.isLoading ? (
          <Center>
            <Random1619 />
          </Center>
        ) : query.data ? (
          <>
            <div className="flex space-x-2">
              <Input value={query.data.name} disabled />
              <Button
                onClick={() => {
                  if (!query.data?.id) return;

                  eden.akasha
                    .webhook({ webhookId: query.data.id })
                    .delete()
                    .then(({ error }) => {
                      if (!error) {
                        query.refetch();
                      } else {
                        toast.warning("webhook delete error");
                      }
                    });
                }}
              >
                알림 삭제
              </Button>
            </div>
          </>
        ) : (
          <>
            <form
              className="flex flex-col space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                e.stopPropagation();
                form.handleSubmit().catch((err) => {
                  toast.warning(err.message);
                });
              }}
            >
              <div className="flex flex-col space-y-5">
                <form.Field
                  name="name"
                  validators={{
                    onChange: ({ value }) =>
                      !value
                        ? "웹훅 이름을 입력해주세요"
                        : value.length < 1 || value.length > 255
                          ? "알림 이름은 최소 1자에서 최대 255까지 입력할 수 있습니다"
                          : undefined,
                    onChangeAsyncDebounceMs: 500,
                  }}
                  children={(field) => {
                    return (
                      <div className="grid w-full items-center gap-1">
                        <Label htmlFor={field.name}>이름</Label>
                        <Input
                          id={field.name}
                          name={field.name}
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                        />
                        <FieldInfo field={field} />
                      </div>
                    );
                  }}
                />

                <form.Field
                  name="provider"
                  validators={{
                    onChange: ({ value }) => (!value ? "웹훅 제공자를 선택해주세요" : undefined),
                    onChangeAsyncDebounceMs: 500,
                  }}
                  children={(f) => {
                    return (
                      <div className="grid w-full items-center gap-1">
                        <Label htmlFor={f.name}>웹훅 제공자</Label>
                        <Select value={f.state.value} onValueChange={(v) => f.handleChange(v)}>
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectGroup>
                              <SelectLabel>웹훅 제공자</SelectLabel>
                              <SelectItem value="discord">
                                <DiscordIcon />
                                Discord
                              </SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      </div>
                    );
                  }}
                />

                <form.Field
                  name="url"
                  validators={{
                    onChange: ({ value }) =>
                      !Validator.url(value) ? "올바른 웹훅 URL을 입력해주세요" : undefined,
                    onChangeAsyncDebounceMs: 500,
                  }}
                  children={(f) => {
                    return (
                      <div className="grid w-full items-center gap-1">
                        <Label htmlFor={f.name}>웹훅 URL</Label>
                        <Input
                          id={f.name}
                          name={f.name}
                          value={f.state.value}
                          onBlur={f.handleBlur}
                          onChange={(e) => f.handleChange(e.target.value)}
                        />
                        <FieldInfo field={f} />
                      </div>
                    );
                  }}
                />
              </div>

              <div className="flex justify-end">
                <form.Subscribe
                  selector={(state) => [state.canSubmit, state.isSubmitting]}
                  children={([canSubmit, isSubmitting]) => (
                    <Button className="w-16" type="submit" disabled={!canSubmit}>
                      {isSubmitting ? <Loader2Icon className="animate-spin" /> : t("g.continue")}
                    </Button>
                  )}
                />
              </div>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
