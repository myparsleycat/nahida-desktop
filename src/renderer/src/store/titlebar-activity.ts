import type { LucideIcon } from "lucide-react";
import { createStore, useStore } from "zustand";

export type TitlebarActivityStatus = "running" | "paused" | "error";

export type TitlebarActivity = {
    id: string;
    label: string;
    status: TitlebarActivityStatus;
    icon: LucideIcon;
    detail?: string;
    tooltip?: string;
    progress?: number;
    order?: number;
    href?: string;
    onClick?: () => void;
};

type TitlebarActivityStore = {
    activities: Record<string, TitlebarActivity>;
    upsertActivity: (activity: TitlebarActivity) => void;
    patchActivity: (id: string, partial: Partial<Omit<TitlebarActivity, "id">>) => void;
    removeActivity: (id: string) => void;
};

function isSameActivity(a: TitlebarActivity, b: TitlebarActivity) {
    return (
        a.id === b.id &&
        a.label === b.label &&
        a.status === b.status &&
        a.icon === b.icon &&
        a.detail === b.detail &&
        a.tooltip === b.tooltip &&
        a.progress === b.progress &&
        a.order === b.order &&
        a.href === b.href &&
        a.onClick === b.onClick
    );
}

export const titlebarActivityStore = createStore<TitlebarActivityStore>((set) => ({
    activities: {},
    upsertActivity: (activity) =>
        set((state) => {
            const current = state.activities[activity.id];
            if (current && isSameActivity(current, activity)) return state;
            return {
                activities: {
                    ...state.activities,
                    [activity.id]: activity,
                },
            };
        }),
    patchActivity: (id, partial) =>
        set((state) => {
            const current = state.activities[id];
            if (!current) return state;
            const next = { ...current, ...partial };
            if (isSameActivity(current, next)) return state;
            return {
                activities: {
                    ...state.activities,
                    [id]: next,
                },
            };
        }),
    removeActivity: (id) =>
        set((state) => {
            if (!(id in state.activities)) return state;
            const { [id]: _, ...activities } = state.activities;
            return { activities };
        }),
}));

export function useTitlebarActivityStore<T>(selector: (state: TitlebarActivityStore) => T): T {
    return useStore(titlebarActivityStore, selector);
}

export function listTitlebarActivities(activities: Record<string, TitlebarActivity>) {
    return Object.values(activities).sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
}
