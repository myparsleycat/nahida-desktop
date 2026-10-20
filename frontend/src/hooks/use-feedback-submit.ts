import { type Request, type Result, Service as Feedback } from "@bindings/feedback";
import { formatSize, toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const ERROR_CODES = [
    "FEEDBACK_UNSUPPORTED_FILE",
    "FEEDBACK_MEDIA_TOO_LARGE",
    "FEEDBACK_TOO_MANY_MEDIA",
    "FEEDBACK_TOO_MANY_MOD_FILES",
    "FEEDBACK_EMPTY_MOD",
    "FEEDBACK_BUSY",
] as const;

/**
 * Sends feedback reports against the server's upload rules, with the upload progress of the report being sent
 * and toasts for its outcome.
 */
export function useFeedbackSubmit() {
    const { t, i18n } = useTranslation();
    const [progress, setProgress] = useState<number | null>(null);
    const cancelled = useRef(false);
    const rulesQuery = useQuery({
        queryKey: ["feedback", "rules"],
        queryFn: () => Feedback.Rules(),
    });
    const rules = rulesQuery.data;

    useEffect(() => {
        return Events.On("feedback:progress", (event) => {
            const data = event.data as { uploadedBytes: number; totalBytes: number };
            setProgress((current) =>
                current === null
                    ? current
                    : data.totalBytes > 0
                      ? (data.uploadedBytes / data.totalBytes) * 100
                      : 100,
            );
        });
    }, []);

    // The first bound the report breaks, as the message to show, or null for a report the server takes.
    const problem = (report: { body: string; mediaFiles: number; mediaBytes: number }) =>
        !rules
            ? rulesQuery.isError
                ? t("page.setting.feedback.error.rules_unavailable")
                : null
            : report.body.length > rules.maxBodyLength
              ? t("page.setting.feedback.error.body_too_long", {
                    max: rules.maxBodyLength.toLocaleString(),
                })
              : report.mediaFiles > rules.maxMediaFiles
                ? t("page.setting.feedback.error.FEEDBACK_TOO_MANY_MEDIA", {
                      count: rules.maxMediaFiles,
                  })
                : report.mediaBytes > rules.maxMediaBytes
                  ? t("page.setting.feedback.error.FEEDBACK_MEDIA_TOO_LARGE", {
                        size: formatSize(rules.maxMediaBytes),
                    })
                  : null;

    // Resolves to the stored report, or null when it failed or was cancelled; a failure is already shown.
    const submit = async (request: Omit<Request, "locale">): Promise<Result | null> => {
        if (!rules) return null;
        cancelled.current = false;
        setProgress(0);
        try {
            const result = await Feedback.Submit({ ...request, locale: i18n.language });
            toast.success(t("page.setting.feedback.sent"), {
                description:
                    result.skippedModFiles > 0
                        ? t("page.setting.feedback.skipped_mod_files", {
                              count: result.skippedModFiles,
                          })
                        : undefined,
            });
            return result;
        } catch (error) {
            if (cancelled.current) return null;
            const message = toErrorMessage(error);
            const code = ERROR_CODES.find((item) => message.startsWith(`${item}:`));
            toast.error(t("page.setting.feedback.failed"), {
                description: code
                    ? t(`page.setting.feedback.error.${code}`, {
                          name: message.slice(code.length + 1),
                          count:
                              code === "FEEDBACK_TOO_MANY_MEDIA"
                                  ? rules.maxMediaFiles
                                  : rules.maxModFiles,
                          size: formatSize(rules.maxMediaBytes),
                      })
                    : message,
            });
            return null;
        } finally {
            setProgress(null);
        }
    };

    const cancel = () => {
        cancelled.current = true;
        void Feedback.Cancel();
    };

    return {
        rules,
        rulesFailed: rulesQuery.isError && !rulesQuery.isFetching,
        retryRules: () => void rulesQuery.refetch(),
        progress,
        sending: progress !== null,
        problem,
        submit,
        cancel,
    };
}
