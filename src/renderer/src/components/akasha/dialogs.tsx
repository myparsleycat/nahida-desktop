// oxlint-disable react/no-children-prop
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
import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { Field, FieldError } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { useDialogStore, useSelectionStore } from "@renderer/store/drive";
import type { Content } from "@shared/types";
import { useForm } from "@tanstack/react-form";
import { useMutation } from "@tanstack/react-query";
import { useLocation, useRouteContext } from "@tanstack/react-router";
import { t } from "i18next";
import { Loader2Icon } from "lucide-react";
import { useEffect } from "react";
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

function getRenameDefaultValues(item?: Content) {
  if (!item) {
    return {
      name: "",
      ext: "",
    };
  }

  if (!item.isDir && item.name.includes(".")) {
    return {
      name: item.name.split(".").slice(0, -1).join("."),
      ext: `.${item.name.split(".").pop()}`,
    };
  }

  return {
    name: item.name,
    ext: "",
  };
}

export function RenameDialog() {
  const { t } = useTranslation();
  const dialog = useDialogStore();
  const selection = useSelectionStore();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();

  const id = location.pathname.split("/").pop() || "";
  const selectedItem = selection.selectedItems[0];
  const defaultRenameValues = getRenameDefaultValues(selectedItem);

  const mutation = useMutation({
    mutationKey: ["drive", "rename", id],
    mutationFn: async ({ item, rename }: { item: Content; rename: string }) => {
      const data = await window.api.invoke("drive:patch:rename", item.id, rename);
      return data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries();
      dialog.setOpen("renameDialog", false);
      return t("page.drive.dialog.rename.#.toast-promise.success");
    },
    onError: (err) => {
      if (err.message.includes("INVALID_WINDOWS_FILENAME")) {
        toast.warning(
          selection.selectedItems[0].isDir
            ? t("page.drive.dialog.common.invalid_dir_name")
            : t("page.drive.dialog.common.invalid_file_name"),
        );
      } else {
        toast.error("Rename Error", {
          description: err.message,
        });
      }
    },
  });

  const form = useForm({
    defaultValues: defaultRenameValues,
    onSubmit: async ({ value }) => {
      if (!selectedItem) {
        return;
      }

      const rename = `${value.name}${value.ext}`;
      const validateResult = ValidateName(rename);
      if (validateResult) {
        toast.warning(t("page.drive.dialog.rename.#.rename.1"), {
          description: validateResult,
        });
        return;
      }

      await mutation.mutateAsync({
        item: selectedItem,
        rename,
      });
    },
  });

  useEffect(() => {
    form.reset(defaultRenameValues);
  }, [defaultRenameValues, form]);

  if (selectedItem) {
    return (
      <Dialog
        open={dialog.renameDialog.open}
        onOpenChange={(v) => {
          if (!v) {
            form.reset(defaultRenameValues);
          }
          dialog.setOpen("renameDialog", v);
        }}
      >
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t("page.drive.dialog.rename.title")}</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col space-y-4"
            autoComplete="off"
            onSubmit={(e) => {
              e.preventDefault();
              void form.handleSubmit();
            }}
          >
            <form.Field
              name="name"
              validators={{
                onChange: ({ value }) =>
                  value.trim() ? undefined : t("page.drive.dialog.rename.#.rename.0"),
              }}
              children={(field) => (
                <Field>
                  <div className="flex flex-row gap-x-4">
                    <Input
                      className="h-10"
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("page.drive.dialog.rename.name_input_placeholder")}
                      maxLength={200}
                      required
                    />
                    {!selectedItem.isDir && (
                      <form.Field
                        name="ext"
                        children={(extField) => (
                          <Input
                            className="h-10 w-1/4"
                            value={extField.state.value}
                            onBlur={extField.handleBlur}
                            onChange={(e) => extField.handleChange(e.target.value)}
                            placeholder={t("page.drive.dialog.rename.extension")}
                            maxLength={50}
                          />
                        )}
                      />
                    )}
                  </div>
                  {field.state.meta.isTouched && !field.state.meta.isValid ? (
                    <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                  ) : null}
                </Field>
              )}
            />

            <div className="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2">
              <Button
                type="button"
                variant="outline"
                onClick={async (e) => {
                  e.preventDefault();
                  form.reset(defaultRenameValues);
                  dialog.setOpen("renameDialog", false, undefined);
                }}
              >
                {t("g.cancel")}
              </Button>
              <form.Subscribe
                selector={(state) => [state.canSubmit, state.isSubmitting]}
                children={([canSubmit, isSubmitting]) => (
                  <Button type="submit" disabled={!canSubmit || mutation.isPending || isSubmitting}>
                    {t("g.confirm")}
                  </Button>
                )}
              />
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

  const id = location.pathname.split("/").pop();

  const mutation = useMutation({
    mutationKey: ["akasha", "make_dir", id],
    mutationFn: async (name: string) => {
      if (!id) {
        toast.error("cannot get current id");
        return;
      }
      await window.api.invoke("drive:post:dir", id, name);
    },
    onSuccess: async () => {
      toast.success(t("page.drive.dialog.create_dir.#.toast-promise.success"));
      dialog.setOpen("createDirDialog", false);
      await queryClient.invalidateQueries();
    },
    onError: (err) => {
      if (err.message.includes("INVALID_WINDOWS_FILENAME")) {
        toast.warning(t("page.drive.dialog.common.invalid_dir_name"));
      } else {
        toast.error("New Directory Error", {
          description: err.message,
        });
      }
    },
  });
  const form = useForm({
    defaultValues: {
      name: "",
    },
    onSubmit: async ({ value }) => {
      const validateResult = ValidateName(value.name);
      if (validateResult) {
        toast.warning(t("page.drive.dialog.create_dir.#.0"), {
          description: validateResult,
        });
        return;
      }

      if (contents.some((item) => item.isDir && item.name === value.name)) {
        toast.warning(t("page.drive.dialog.create_dir.#.2"));
        return;
      }

      await mutation.mutateAsync(value.name);
    },
  });

  return (
    <Dialog
      open={dialog.createDirDialog.open}
      onOpenChange={(v) => {
        if (!v) {
          form.reset();
        }
        dialog.setOpen("createDirDialog", v);
      }}
    >
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t("page.drive.dialog.create_dir.title")}</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col space-y-4"
          autoComplete="off"
          onSubmit={(e) => {
            e.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Field
            name="name"
            validators={{
              onChange: ({ value }) =>
                value.trim() ? undefined : t("page.drive.dialog.create_dir.#.0"),
            }}
            children={(field) => (
              <Field>
                <Input
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("page.drive.dialog.create_dir.name_input_placeholder")}
                  maxLength={255}
                  required
                />
                {field.state.meta.isTouched && !field.state.meta.isValid ? (
                  <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                ) : null}
              </Field>
            )}
          />
          <div className="flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2">
            <Button
              type="button"
              variant="outline"
              onClick={(e) => {
                e.preventDefault();
                form.reset();
                dialog.setOpen("createDirDialog", false);
              }}
            >
              {t("g.cancel")}
            </Button>
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
              children={([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  className="flex items-center gap-2"
                  disabled={!canSubmit || mutation.isPending || isSubmitting}
                >
                  {mutation.isPending && <Loader2Icon className="animate-spin" />}
                  {t("g.confirm")}
                </Button>
              )}
            />
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// export function PubLinkDialog() {
//   const dialog = useDialogStore();
//   const { t } = useTranslation();
//   type ShareLink = {
//     id: string;
//     password: string | null;
//     expires_at?: string | null;
//     url: string;
//   };

//   const itemId = useMemo(() => {
//     return dialog.shareDialog.data?.id as string | undefined;
//   }, [dialog.shareDialog.data]);

//   const [pubLinkSwitchChecked, setPubLinkSwitch] = useState(false);

//   const query = useQuery({
//     queryKey: ["akasha", "drive", "publink", itemId],
//     queryFn: async () => {
//       if (!itemId) {
//         throw new Error("ID를 가져오는데 실패함");
//       }

//       const { data, error } = await eden.akasha.content.share({ id: itemId }).get();

//       if (error) {
//         throw new Error(error.value.toString());
//       }

//       return data;
//     },
//     enabled: !!itemId,
//   });

//   const IDISEMPTY = "아이템 ID를 가져오는데 실패함";
//   const passwordForm = useForm({
//     defaultValues: {
//       password: "",
//     },
//     onSubmit: async ({ value }) => {
//       const link = (query.data?.link ?? null) as ShareLink | null;
//       if (!link) {
//         toast.warning(IDISEMPTY);
//         return;
//       }

//       await pwdMutation.mutateAsync({
//         id: link.id,
//         bool: !!link.password,
//         password: value.password,
//       });

//       toast.success(link.password ? "비밀번호가 제거되었습니다" : "비밀번호가 생성되었습니다");
//       passwordForm.reset();
//       await query.refetch();
//     },
//   });
//   const expirationForm = useForm({
//     defaultValues: {
//       date: undefined as Date | undefined,
//     },
//     onSubmit: async ({ value }) => {
//       const link = (query.data?.link ?? null) as ShareLink | null;
//       if (!query.data?.id || !link) {
//         toast.warning(IDISEMPTY);
//         return;
//       }

//       const nextValue = value.date ? format(value.date, "yyyy-MM-dd") : "";

//       await linkExpiresPatchMutation.mutateAsync({
//         id: link.id,
//         value: nextValue,
//       });

//       toast.success(nextValue ? "만료일이 설정되었습니다" : "만료일이 해제되었습니다");
//       await query.refetch();
//     },
//   });

//   useEffect(() => {
//     if (query.data) {
//       const link = (query.data.link ?? null) as ShareLink | null;
//       setPubLinkSwitch(!!query.data.link);
//       expirationForm.reset({
//         date: link?.expires_at ? new Date(link.expires_at) : undefined,
//       });
//     }
//   }, [expirationForm, query.data]);

//   const changePermissionMutation = useMutation({
//     mutationKey: ["akasha", "drive", "pub-link-dialog", "permission", "change"],
//     mutationFn: async (pid: string) => {
//       if (!query.data?.id) {
//         throw new Error(IDISEMPTY);
//       }

//       const { data, error } = await eden.akasha.content.share
//         .permission({ id: query.data.id })
//         .p({ pid })
//         .patch();

//       if (error) {
//         throw new Error(error.value.toString());
//       }

//       return data.r;
//     },
//   });

//   const handleChangePermissionBtn = async (pid: string) => {
//     return changePermissionMutation
//       .mutateAsync(pid)
//       .then(() => {
//         toast.success("권한이 변경되었습니다");
//         void query.refetch();
//       })
//       .catch((err) => {
//         toast.error("권한 변경 중 오류 발생", {
//           description: err.message,
//         });
//       });
//   };

//   const deletePermissionMutation = useMutation({
//     mutationKey: ["akasha", "drive", "pub-link-dialog", "permission", "delete"],
//     mutationFn: async (pid: string) => {
//       if (!query.data?.id) {
//         throw new Error(IDISEMPTY);
//       }

//       const { error } = await eden.akasha.content.share
//         .permission({ id: query.data.id })
//         .p({ pid })
//         .delete();

//       if (error) {
//         throw new Error(error.value.toString());
//       }

//       return true;
//     },
//   });

//   const handleDeletePermissionBtn = async (pid: string) => {
//     return deletePermissionMutation
//       .mutateAsync(pid)
//       .then(() => {
//         toast.success("권한이 제거되었습니다");
//         void query.refetch();
//       })
//       .catch((err) => {
//         toast.error("권한 삭제 중 오류 발생", {
//           description: err.message,
//         });
//       });
//   };

//   const handleInviteUrlBtn = async () => {
//     if (!query.data?.id) {
//       toast.error(IDISEMPTY);
//       return;
//     }

//     const { data, error } = await eden.akasha.content.share
//       .permission({ id: query.data.id })
//       .invite_url.get();

//     if (error) {
//       return toast.warning("초대 URL 가져오는중 오류 발생", {
//         description: error.value.toString(),
//       });
//     }

//     await window.api.invoke("util:copyStr", data.url);
//     return;
//   };

//   const pwdMutation = useMutation({
//     mutationKey: ["akasha", "drive", "pub-link-dialog", "link", "password"],
//     mutationFn: async ({ id, bool, password }: { id: string; bool: boolean; password: string }) => {
//       const { data, error } = await eden.akasha.content.share.link({ id }).password.patch({
//         value: bool ? null : password,
//       });

//       if (error) {
//         throw new Error(error.value.toString());
//       }

//       return data;
//     },
//   });

//   const linkExpiresPatchMutation = useMutation({
//     mutationKey: ["akasha", "drive", "pub-link-dialog", "link", "expires"],
//     mutationFn: async ({ id, value }: { id: string; value: string | undefined }) => {
//       const { data, error } = await eden.akasha.content.share.link({ id }).expires.patch({
//         value: value || null,
//       });

//       if (error) {
//         throw new Error(error.value.toString());
//       }

//       return data;
//     },
//   });

//   const handlePubLinkSwitch = async () => {
//     const link = (query.data?.link ?? null) as ShareLink | null;

//     if (!link) {
//       const { error } = await eden.akasha.content.share.link.post(null, {
//         query: { item_id: itemId! },
//       });

//       if (error) {
//         setPubLinkSwitch(false);
//         return toast.error(error.value.toString());
//       }
//     } else {
//       const { error } = await eden.akasha.content.share.link({ id: link.id }).delete();

//       if (error) {
//         setPubLinkSwitch(true);
//         return toast.warning(error.value.toString());
//       }
//     }

//     toast.success(!query.data?.link ? "공개 링크가 생성되었습니다" : "공개 링크가 제거되었습니다");

//     await query.refetch();
//     return;
//   };

//   return (
//     <Dialog
//       open={dialog.shareDialog.open}
//       onOpenChange={(v) => {
//         if (!v) {
//           passwordForm.reset();
//           expirationForm.reset({ date: undefined });
//         }
//         dialog.setOpen("shareDialog", v);
//       }}
//     >
//       <DialogTitle></DialogTitle>
//       <DialogContent aria-describedby={undefined}>
//         {query.data ? (
//           <div className="flex flex-col gap-y-8 w-full">
//             <div className="w-full">
//               <Label>액세스 권한이 있는 사용자</Label>
//               <div className="flex flex-row mt-2 items-center space-x-4">
//                 {query.data.permissions.length > 0 ? (
//                   <div className="grid grid-cols-8 gap-4 select-none max-h-28 overflow-y-auto overflow-x-hidden p-1">
//                     {query.data.permissions.map((permission) => (
//                       <div key={permission.id} className="flex">
//                         <DropdownMenu>
//                           <DropdownMenuTrigger>
//                             <Avatar>
//                               <AvatarImage
//                                 src={permission.image || "https://placehold.co/100"}
//                                 alt={permission.name}
//                               />
//                               <AvatarFallback>{permission.name}</AvatarFallback>
//                             </Avatar>
//                           </DropdownMenuTrigger>
//                           <DropdownMenuContent>
//                             <DropdownMenuGroup>
//                               <DropdownMenuLabel>
//                                 {permission.name} (
//                                 {permission.permission === "VIEW"
//                                   ? "조회 가능"
//                                   : permission.permission === "EDIT"
//                                     ? "편집 가능"
//                                     : permission.permission === "UPLOAD"
//                                       ? "업로드 가능"
//                                       : permission.permission}
//                                 )
//                               </DropdownMenuLabel>
//                               <DropdownMenuSeparator />
//                               <DropdownMenuItem
//                                 className="cursor-pointer"
//                                 onClick={async () => handleChangePermissionBtn(permission.id)}
//                               >
//                                 권한 변경
//                               </DropdownMenuItem>
//                               <DropdownMenuItem
//                                 className="cursor-pointer"
//                                 onClick={async () => handleDeletePermissionBtn(permission.id)}
//                               >
//                                 권한 삭제
//                               </DropdownMenuItem>
//                             </DropdownMenuGroup>
//                           </DropdownMenuContent>
//                         </DropdownMenu>
//                       </div>
//                     ))}
//                   </div>
//                 ) : (
//                   <Alert>
//                     <AlertTriangleIcon className="h-4 w-4" />
//                     <AlertTitle>항목이 비어있음</AlertTitle>
//                     <AlertDescription>액세스 권한이 부여된 사용자가 없습니다</AlertDescription>
//                   </Alert>
//                 )}

//                 <div>
//                   <Tooltip delayDuration={50}>
//                     <TooltipTrigger
//                       className={buttonVariants({
//                         variant: "outline",
//                         size: "icon",
//                       })}
//                       onClick={handleInviteUrlBtn}
//                     >
//                       <LinkIcon className="pointer-events-none" />
//                     </TooltipTrigger>
//                     <TooltipContent className="max-w-sm w-full">
//                       <p>
//                         이 항목에 접근 가능한 권한을 부여하는 초대 링크를 복사합니다. 링크는 복사된
//                         시점부터 24시간 동안 사용 가능합니다
//                       </p>
//                     </TooltipContent>
//                   </Tooltip>
//                 </div>
//               </div>
//             </div>

//             <div className="w-full">
//               <Label>일반 액세스</Label>
//               <div className="flex flex-col mt-2">
//                 <div className="flex flex-row items-center gap-4 w-full">
//                   <div className="flex">
//                     {query.data.link ? <EarthIcon color="green" /> : <LockIcon color="orange" />}
//                   </div>
//                   <div>
//                     <p className="text-base">{query.data.link ? <>공유중</> : <>제한됨</>}</p>
//                     <p className="text-sm">
//                       {query.data.link ? (
//                         <>링크가 있는 모든 사용자가 볼 수 있음</>
//                       ) : (
//                         <>액세스 권한이 있는 사용자만 볼 수 있음</>
//                       )}
//                     </p>
//                   </div>
//                   <div className="flex justify-end grow">
//                     <Switch
//                       checked={pubLinkSwitchChecked}
//                       onCheckedChange={setPubLinkSwitch}
//                       onClick={handlePubLinkSwitch}
//                     />
//                   </div>
//                 </div>

//                 {query.data.link &&
//                   (() => {
//                     const link = query.data.link as ShareLink;

//                     return (
//                       <div className="flex flex-col gap-4">
//                         <div className="flex mt-4 space-x-4">
//                           <div className="flex w-full max-w-sm flex-col gap-1.5">
//                             <FieldLabel>{t("g.password")}</FieldLabel>
//                             <form
//                               className="relative"
//                               onSubmit={(e) => {
//                                 e.preventDefault();
//                                 void passwordForm.handleSubmit();
//                               }}
//                             >
//                               <passwordForm.Field
//                                 name="password"
//                                 validators={{
//                                   onChange: ({ value }) => {
//                                     if (link.password) {
//                                       return undefined;
//                                     }

//                                     if (value.length < 4) {
//                                       return "비밀번호는 최소 4자 이상이어야 합니다";
//                                     }

//                                     return undefined;
//                                   },
//                                 }}
//                                 children={(field) => (
//                                   <Field>
//                                     <Input
//                                       type="text"
//                                       disabled={!!link.password}
//                                       required={!link.password}
//                                       minLength={4}
//                                       maxLength={150}
//                                       value={field.state.value}
//                                       onBlur={field.handleBlur}
//                                       onChange={(e) => field.handleChange(e.target.value)}
//                                     />
//                                     {field.state.meta.isTouched && !field.state.meta.isValid ? (
//                                       <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
//                                     ) : null}
//                                   </Field>
//                                 )}
//                               />
//                               <div className="absolute inset-y-0 right-2 flex items-center">
//                                 <passwordForm.Subscribe
//                                   selector={(state) => [state.canSubmit, state.isSubmitting]}
//                                   children={([canSubmit, isSubmitting]) => (
//                                     <button
//                                       type="submit"
//                                       disabled={
//                                         pwdMutation.isPending ||
//                                         isSubmitting ||
//                                         (!link.password && !canSubmit)
//                                       }
//                                     >
//                                       {pwdMutation.isPending ? (
//                                         <Loader2Icon className="size-4 animate-spin" />
//                                       ) : link.password ? (
//                                         <XIcon />
//                                       ) : (
//                                         <SaveIcon />
//                                       )}
//                                     </button>
//                                   )}
//                                 />
//                               </div>
//                             </form>
//                           </div>

//                           <div className="flex w-full max-w-sm flex-col gap-1.5">
//                             <FieldLabel>공유 만료일</FieldLabel>
//                             <form
//                               className="relative"
//                               onSubmit={(e) => {
//                                 e.preventDefault();
//                                 void expirationForm.handleSubmit();
//                               }}
//                             >
//                               <expirationForm.Field
//                                 name="date"
//                                 children={(field) => (
//                                   <DatePicker
//                                     value={field.state.value}
//                                     onChange={(value) => {
//                                       field.handleChange(value);
//                                     }}
//                                     disabled={(date) => {
//                                       const today = new Date();
//                                       today.setHours(0, 0, 0, 0);

//                                       return date <= today;
//                                     }}
//                                   />
//                                 )}
//                               />
//                               <div className="absolute inset-y-0 right-2 flex items-center">
//                                 <button
//                                   type="submit"
//                                   className="pointer-events-auto z-50"
//                                   disabled={linkExpiresPatchMutation.isPending}
//                                 >
//                                   {linkExpiresPatchMutation.isPending ? (
//                                     <Loader2Icon className="size-4 animate-spin" />
//                                   ) : (
//                                     <SaveIcon />
//                                   )}
//                                 </button>
//                               </div>
//                             </form>
//                           </div>
//                         </div>

//                         <div className="flex flex-row gap-x-3">
//                           <input
//                             className={cn(
//                               "w-full rounded-lg border-none bg-black/5 dark:bg-white/5 py-1.5 px-3 text-sm/6",
//                               "focus:outline-hidden data-focus:outline-2 data-focus:-outline-offset-2 data-focus:outline-white/25",
//                             )}
//                             type="link"
//                             id="link"
//                             value={link.url}
//                             readOnly
//                           />
//                           <Button
//                             type="button"
//                             className="aspect-square"
//                             variant="outline"
//                             size="icon"
//                             onClick={() => {
//                               const str = link.url;

//                               if (!str) {
//                                 toast.warning("Cannot found Link URL");
//                                 return;
//                               }

//                               void window.api.invoke("util:copyStr", str);
//                             }}
//                           >
//                             <CopyIcon className="pointer-events-none" />
//                           </Button>
//                         </div>
//                       </div>
//                     );
//                   })()}
//               </div>
//             </div>
//           </div>
//         ) : (
//           <div className="flex justify-center items-center">
//             <LoaderIcon className="animate-spin-1.5" size={40} />
//           </div>
//         )}
//       </DialogContent>
//     </Dialog>
//   );
// }

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
              void window.api.invoke("drive:delete:items", [], "trash").then(() => {
                void queryClient.refetchQueries({
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

export function DeleteItemsDialog() {
  const { t } = useTranslation();
  const { deleteItemsDialog, setOpen } = useDialogStore();
  const { selectedItems, setSelectedItems } = useSelectionStore();
  const { queryClient } = useRouteContext({ from: "__root__" });

  const deleteMutation = useMutation({
    mutationKey: ["akasha", "drive", "delete-items", "delete"],
    mutationFn: async (ids: string[]) => {
      await window.api.invoke("drive:delete:items", ids, "delete");
    },
  });

  const handleDelete = async () => {
    if (selectedItems.length === 0) {
      setOpen("deleteItemsDialog", false);
      return;
    }

    await deleteMutation
      .mutateAsync(selectedItems.map((item) => item.id))
      .then(async () => {
        toast.success(t("page.drive.dialog.delete_items.#.toast.success"));
        setSelectedItems([]);
        setOpen("deleteItemsDialog", false);
        await queryClient.invalidateQueries();
      })
      .catch((err: string) => {
        toast.error(err);
      });
  };

  return (
    <AlertDialog
      open={deleteItemsDialog.open}
      onOpenChange={(v) => setOpen("deleteItemsDialog", v)}
    >
      <AlertDialogContent
        onEscapeKeyDown={(e) => {
          if (deleteMutation.isPending) {
            e.preventDefault();
          }
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{t("page.drive.dialog.delete_items.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("page.drive.dialog.delete_items.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleDelete}
            disabled={deleteMutation.isPending}
          >
            {deleteMutation.isPending && <Loader2Icon className="animate-spin" />}
            {t("page.drive.dialog.delete_items.action")}
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
          </AlertDialogDescription>
          <div className="text-sm mt-2">
            {t("page.drive.dialog.conflict_name.option_suffix")}
            <br />
            {t("page.drive.dialog.conflict_name.option_skip")}

            {preview.length > 0 && (
              <div className="w-full text-left mt-2 text-xs text-muted-foreground">
                <p>{t("page.drive.dialog.conflict_name.preview_label")}</p>
                <p>{preview.join(", ")}</p>
                {hiddenCount > 0 && (
                  <p>{t("page.drive.dialog.conflict_name.preview_more", { count: hiddenCount })}</p>
                )}
              </div>
            )}
          </div>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "cancel");
            }}
          >
            {t("g.cancel")}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              setOpen("conflictNameDialog", false);
              resolveDialog("conflictNameDialog", "skip");
            }}
          >
            {t("page.drive.dialog.conflict_name.action_skip")}
          </AlertDialogAction>
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

// export function NotiDialog() {
//   const { t } = useTranslation();
//   const { notiDialog, setOpen } = useDialogStore();

//   const query = useQuery({
//     queryKey: ["akasha", "notiDialog", notiDialog.data?.id],
//     queryFn: async () => {
//       const { data, error } = await eden.akasha.webhook
//         .item({ itemId: notiDialog.data.id as string })
//         .get();

//       if (error) {
//         if (error.status === 404) {
//           return null;
//         }

//         throw new Error(error.value.toString());
//       }

//       return data;
//     },
//     enabled: !!notiDialog.data?.id,
//   });

//   const form = useForm({
//     defaultValues: {
//       name: "",
//       provider: "discord",
//       url: "",
//     },
//     onSubmit: async ({ value }) => {
//       const { name, provider, url } = value;

//       const link = notiDialog.data?.link;
//       const srcId = notiDialog.data?.id;
//       if (!srcId) {
//         throw new Error("selecton item is empty");
//       }

//       const { data, error } = await eden.akasha.webhook.post({
//         name,
//         srcId,
//         // @ts-ignore
//         provider,
//         webhookUrl: url,
//         ...(link && { link }),
//       });

//       if (error) {
//         throw new Error(error.value.toString());
//       }

//       await query.refetch();

//       return data;
//     },
//   });

//   useEffect(() => {
//     if (notiDialog.open) {
//       form.reset();
//     }
//   }, [notiDialog.open, form]);

//   function FieldInfo({ field }: { field: AnyFieldApi }) {
//     return (
//       <>
//         {field.state.meta.isTouched && !field.state.meta.isValid ? (
//           <em className="text-destructive">{field.state.meta.errors.join(", ")}</em>
//         ) : null}
//         {field.state.meta.isValidating ? "Validating..." : null}
//       </>
//     );
//   }

//   return (
//     <Dialog open={notiDialog.open} onOpenChange={(v) => setOpen("notiDialog", v)}>
//       <DialogContent aria-describedby={undefined} showCloseButton={false}>
//         <VisuallyHidden>
//           <DialogTitle></DialogTitle>
//         </VisuallyHidden>

//         {query.isLoading ? (
//           <Center>
//             <Random1619 />
//           </Center>
//         ) : query.data ? (
//           <>
//             <div className="flex space-x-2">
//               <Input value={query.data.name} disabled />
//               <Button
//                 onClick={() => {
//                   if (!query.data?.id) return;

//                   void eden.akasha
//                     .webhook({ webhookId: query.data.id })
//                     .delete()
//                     .then(({ error }) => {
//                       if (!error) {
//                         void query.refetch();
//                       } else {
//                         toast.warning("webhook delete error");
//                       }
//                     });
//                 }}
//               >
//                 알림 삭제
//               </Button>
//             </div>
//           </>
//         ) : (
//           <>
//             <form
//               className="flex flex-col space-y-4"
//               onSubmit={(e) => {
//                 e.preventDefault();
//                 e.stopPropagation();
//                 form.handleSubmit().catch((err) => {
//                   toast.warning(err.message);
//                 });
//               }}
//             >
//               <div className="flex flex-col space-y-5">
//                 <form.Field
//                   name="name"
//                   validators={{
//                     onChange: ({ value }) =>
//                       !value
//                         ? "웹훅 이름을 입력해주세요"
//                         : value.length < 1 || value.length > 255
//                           ? "알림 이름은 최소 1자에서 최대 255까지 입력할 수 있습니다"
//                           : undefined,
//                     onChangeAsyncDebounceMs: 500,
//                   }}
//                   children={(field) => {
//                     return (
//                       <div className="grid w-full items-center gap-1">
//                         <Label htmlFor={field.name}>이름</Label>
//                         <Input
//                           id={field.name}
//                           name={field.name}
//                           value={field.state.value}
//                           onBlur={field.handleBlur}
//                           onChange={(e) => field.handleChange(e.target.value)}
//                         />
//                         <FieldInfo field={field} />
//                       </div>
//                     );
//                   }}
//                 />

//                 <form.Field
//                   name="provider"
//                   validators={{
//                     onChange: ({ value }) => (!value ? "웹훅 제공자를 선택해주세요" : undefined),
//                     onChangeAsyncDebounceMs: 500,
//                   }}
//                   children={(f) => {
//                     return (
//                       <div className="grid w-full items-center gap-1">
//                         <Label htmlFor={f.name}>웹훅 제공자</Label>
//                         <Select value={f.state.value} onValueChange={(v) => f.handleChange(v)}>
//                           <SelectTrigger className="w-full">
//                             <SelectValue />
//                           </SelectTrigger>
//                           <SelectContent>
//                             <SelectGroup>
//                               <SelectLabel>웹훅 제공자</SelectLabel>
//                               <SelectItem value="discord">
//                                 <DiscordIcon />
//                                 Discord
//                               </SelectItem>
//                             </SelectGroup>
//                           </SelectContent>
//                         </Select>
//                       </div>
//                     );
//                   }}
//                 />

//                 <form.Field
//                   name="url"
//                   validators={{
//                     onChange: ({ value }) =>
//                       !isURL(value) ? "올바른 웹훅 URL을 입력해주세요" : undefined,
//                     onChangeAsyncDebounceMs: 500,
//                   }}
//                   children={(f) => {
//                     return (
//                       <div className="grid w-full items-center gap-1">
//                         <Label htmlFor={f.name}>웹훅 URL</Label>
//                         <Input
//                           id={f.name}
//                           name={f.name}
//                           value={f.state.value}
//                           onBlur={f.handleBlur}
//                           onChange={(e) => f.handleChange(e.target.value)}
//                         />
//                         <FieldInfo field={f} />
//                       </div>
//                     );
//                   }}
//                 />
//               </div>

//               <div className="flex justify-end">
//                 <form.Subscribe
//                   selector={(state) => [state.canSubmit, state.isSubmitting]}
//                   children={([canSubmit, isSubmitting]) => (
//                     <Button className="w-16" type="submit" disabled={!canSubmit}>
//                       {isSubmitting ? <Loader2Icon className="animate-spin" /> : t("g.continue")}
//                     </Button>
//                   )}
//                 />
//               </div>
//             </form>
//           </>
//         )}
//       </DialogContent>
//     </Dialog>
//   );
// }
