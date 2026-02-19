/** biome-ignore-all lint/correctness/noChildrenProp: _ */
import { Titlebar } from "@renderer/components/titlebar";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Field, FieldGroup, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { Textarea } from "@renderer/components/ui/textarea";
import { useForm } from "@tanstack/react-form";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/report")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const [nid, setnid] = useState("");

  const form = useForm({
    defaultValues: {
      title: "",
      description: "",
      submitLog: "true",
    },
    onSubmit: async ({ value }) => {
      const { nid } = await window.api.invoke("util:submitReport", {
        title: value.title,
        description: value.description,
        submitLog: value.submitLog === "true",
      });
      setnid(nid);
    },
  });

  if (nid) {
    return (
      <div className="flex h-[calc(100vh-28px)] items-center justify-center">
        <Titlebar title={{ text: t("page.report.title"), position: "center" }} />

        <div className="flex flex-col justify-center items-center space-y-2">
          <h1>{t("page.report.reported_title")}</h1>
          <p>{t("page.report.report_id", { id: nid })}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-[calc(100vh-28px)] flex-col">
      <Titlebar title={{ text: t("page.report.title"), position: "center" }} />

      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          form.handleSubmit();
        }}
        className="flex flex-1 flex-col space-y-6 p-8 text-sm"
      >
        <div className="flex flex-col">
          <h1 className="text-lg">{t("page.report.title")}</h1>
          <p>{t("page.report.description_label")}</p>
        </div>

        <form.Field
          name="title"
          children={({ name, state, handleChange, handleBlur }) => (
            <Field>
              <FieldLabel htmlFor={name}>{t("page.report.fields.title")}</FieldLabel>
              <Input
                className="text-sm"
                id={name}
                value={state.value}
                onChange={(e) => handleChange(e.target.value)}
                onBlur={handleBlur}
                placeholder={t("page.report.fields.title_placeholder")}
                maxLength={120}
              />
            </Field>
          )}
        />

        <form.Field
          name="description"
          children={({ name, state, handleChange, handleBlur }) => (
            <Field className="flex flex-1 flex-col">
              <FieldLabel htmlFor={name}>{t("page.report.fields.description")}</FieldLabel>
              <Textarea
                value={state.value}
                onChange={(e) => handleChange(e.target.value)}
                onBlur={handleBlur}
                className="flex-1 resize-none text-sm"
                id={name}
                required
                maxLength={2000}
              />
            </Field>
          )}
        />

        <form.Field
          name="submitLog"
          children={({ name, handleChange }) => (
            <FieldGroup className="w-full">
              <Field orientation="horizontal">
                <Checkbox
                  id={name}
                  defaultChecked
                  onCheckedChange={(checked) => {
                    if (checked) {
                      handleChange("true");
                    } else {
                      handleChange("false");
                    }
                  }}
                />
                <FieldLabel htmlFor={name}>{t("page.report.fields.attach_log")}</FieldLabel>
              </Field>
            </FieldGroup>
          )}
        />

        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
          children={([canSubmit, isSubmitting]) => (
            <Button type="submit" disabled={!canSubmit} className="w-full">
              {isSubmitting && <Loader2Icon className="animate-spin" />}
              {t("page.report.submit")}
            </Button>
          )}
        />
      </form>
    </div>
  );
}
