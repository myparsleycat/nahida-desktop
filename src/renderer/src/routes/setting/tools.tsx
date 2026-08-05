// vision-llm disabled — LLM settings card isolated. Page renders empty until restored.
//
// import { Button } from "@renderer/components/ui/button";
// import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
// import { Input } from "@renderer/components/ui/input";
// import {
//   Select,
//   SelectContent,
//   SelectGroup,
//   SelectItem,
//   SelectTrigger,
//   SelectValue,
// } from "@renderer/components/ui/select";
// import { useSettings } from "@renderer/hooks/use-settings";
// import {
//   TOUCH_PROFILE_LLM_PROTOCOLS,
//   TOUCH_PROFILE_LLM_REASONING_LEVELS,
//   type TouchProfileLlmProtocol,
//   type TouchProfileLlmReasoning,
// } from "@shared/touch-profile-llm";
import { createFileRoute } from "@tanstack/react-router";
// import { useEffect, useState } from "react";
// import { useTranslation } from "react-i18next";
// import { toast } from "sonner";

export const Route = createFileRoute("/setting/tools")({
  component: RouteComponent,
});

// const settingsConfig = {
//   touchProfileLlmProtocol: "tools.touchProfileLlmProtocol",
//   touchProfileLlmEndpoint: "tools.touchProfileLlmEndpoint",
//   touchProfileLlmModel: "tools.touchProfileLlmModel",
//   touchProfileLlmReasoning: "tools.touchProfileLlmReasoning",
// } as const;

function RouteComponent() {
  // vision-llm disabled — original LLM settings UI isolated below
  // const { t } = useTranslation();
  // const { settings, update, setSettings, isLoading } = useSettings(settingsConfig);
  // const [apiKey, setApiKey] = useState("");
  // const [apiKeyConfigured, setApiKeyConfigured] = useState(false);
  // const [apiKeyLoading, setApiKeyLoading] = useState(false);

  // useEffect(() => {
  //   let cancelled = false;
  //   void window.api
  //     .invoke("tools:touchProfileGetLlmSettings")
  //     .then((next) => {
  //       if (!cancelled) setApiKeyConfigured(next.apiKeyConfigured);
  //     })
  //     .catch((error) => {
  //       if (!cancelled) {
  //         toast.error(t("page.setting.tools.touch_profile.llm.api_key.api_key_load_failed"), {
  //           description: String(error),
  //         });
  //       }
  //     });
  //   return () => {
  //     cancelled = true;
  //   };
  // }, [t]);

  // if (isLoading) {
  //   return null;
  // }

  // const handleLlmTextBlur = async (key: "touchProfileLlmEndpoint" | "touchProfileLlmModel") => {
  //   await update(key, settings[key]);
  // };

  // const handleSaveApiKey = async () => {
  //   if (!apiKey.trim() || apiKeyLoading) return;
  //   setApiKeyLoading(true);
  //   try {
  //     const next = await window.api.invoke("tools:touchProfileSetLlmApiKey", {
  //       apiKey,
  //     });
  //     setApiKey("");
  //     setApiKeyConfigured(next.apiKeyConfigured);
  //     toast.success(t("page.setting.tools.touch_profile.llm.api_key.api_key_saved"));
  //   } catch (error) {
  //     toast.error(t("page.setting.tools.touch_profile.llm.api_key.api_key_save_failed"), {
  //       description: String(error),
  //     });
  //   } finally {
  //     setApiKeyLoading(false);
  //   }
  // };

  // const handleClearApiKey = async () => {
  //   if (apiKeyLoading) return;
  //   setApiKeyLoading(true);
  //   try {
  //     const next = await window.api.invoke("tools:touchProfileClearLlmApiKey");
  //     setApiKeyConfigured(next.apiKeyConfigured);
  //     toast.success(t("page.setting.tools.touch_profile.llm.api_key.api_key_cleared"));
  //   } catch (error) {
  //     toast.error(t("page.setting.tools.touch_profile.llm.api_key.api_key_clear_failed"), {
  //       description: String(error),
  //     });
  //   } finally {
  //     setApiKeyLoading(false);
  //   }
  // };

  return (
    <div className="space-y-6 p-4">
      {/* vision-llm disabled — LLM settings card isolated
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.tools.touch_profile.title")}
          </CardTitle>
          <p className="text-xs text-muted-foreground">
            {t("page.setting.tools.touch_profile.llm.description")}
          </p>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">
              {t("page.setting.tools.touch_profile.llm.protocol.title")}
            </label>
            <Select
              value={settings.touchProfileLlmProtocol}
              onValueChange={(value) => {
                if (
                  !value ||
                  !TOUCH_PROFILE_LLM_PROTOCOLS.includes(value as TouchProfileLlmProtocol)
                ) {
                  return;
                }
                const protocol = value as TouchProfileLlmProtocol;
                setSettings((prev) => ({ ...prev, touchProfileLlmProtocol: protocol }));
                void update("touchProfileLlmProtocol", protocol);
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {TOUCH_PROFILE_LLM_PROTOCOLS.map((protocol) => (
                    <SelectItem key={protocol} value={protocol}>
                      {t(`page.setting.tools.touch_profile.llm.protocol.${protocol}`)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium">
              {t("page.setting.tools.touch_profile.llm.endpoint.title")}
            </label>
            <Input
              value={settings.touchProfileLlmEndpoint}
              onChange={(event) =>
                setSettings((prev) => ({
                  ...prev,
                  touchProfileLlmEndpoint: event.target.value,
                }))
              }
              onBlur={() => void handleLlmTextBlur("touchProfileLlmEndpoint")}
              onKeyDown={(event) => {
                if (event.key === "Enter") event.currentTarget.blur();
              }}
            />
            <p className="text-xs text-muted-foreground">
              {t("page.setting.tools.touch_profile.llm.endpoint.description")}
            </p>
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium">
              {t("page.setting.tools.touch_profile.llm.model.title")}
            </label>
            <Input
              value={settings.touchProfileLlmModel}
              onChange={(event) =>
                setSettings((prev) => ({ ...prev, touchProfileLlmModel: event.target.value }))
              }
              onBlur={() => void handleLlmTextBlur("touchProfileLlmModel")}
              onKeyDown={(event) => {
                if (event.key === "Enter") event.currentTarget.blur();
              }}
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium">
              {t("page.setting.tools.touch_profile.llm.reasoning.title")}
            </label>
            <Select
              value={settings.touchProfileLlmReasoning}
              onValueChange={(value) => {
                if (
                  !value ||
                  !TOUCH_PROFILE_LLM_REASONING_LEVELS.includes(value as TouchProfileLlmReasoning)
                ) {
                  return;
                }
                const reasoning = value as TouchProfileLlmReasoning;
                setSettings((prev) => ({ ...prev, touchProfileLlmReasoning: reasoning }));
                void update("touchProfileLlmReasoning", reasoning);
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {TOUCH_PROFILE_LLM_REASONING_LEVELS.map((reasoning) => (
                    <SelectItem key={reasoning} value={reasoning}>
                      {t(`page.setting.tools.touch_profile.llm.reasoning.${reasoning}`)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium">
              {t("page.setting.tools.touch_profile.llm.api_key.title")}
            </label>
            <div className="flex gap-2">
              <Input
                type="password"
                value={apiKey}
                onChange={(event) => setApiKey(event.target.value)}
                placeholder={
                  apiKeyConfigured
                    ? t("page.setting.tools.touch_profile.llm.api_key.configured")
                    : t("page.setting.tools.touch_profile.llm.api_key.placeholder")
                }
                disabled={apiKeyLoading}
              />
              <Button
                type="button"
                onClick={() => void handleSaveApiKey()}
                disabled={!apiKey.trim() || apiKeyLoading}
              >
                {t("page.setting.tools.touch_profile.llm.api_key.save")}
              </Button>
              {apiKeyConfigured ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void handleClearApiKey()}
                  disabled={apiKeyLoading}
                >
                  {t("page.setting.tools.touch_profile.llm.api_key.clear")}
                </Button>
              ) : null}
            </div>
            <p className="text-xs text-muted-foreground">
              {t("page.setting.tools.touch_profile.llm.api_key.description")}
            </p>
          </div>
        </CardContent>
      </Card> */}
    </div>
  );
}
