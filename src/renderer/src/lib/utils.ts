import { clsx, type ClassValue } from "clsx";
import { format } from "date-fns";
import { enUS, ko, zhCN } from "date-fns/locale";
import { cubicOut } from "svelte/easing";
import type { TransitionConfig } from "svelte/transition";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChild<T> = T extends { child?: any } ? Omit<T, "child"> : T;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChildren<T> = T extends { children?: any } ? Omit<T, "children"> : T;
export type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>;
export type WithElementRef<T, U extends HTMLElement = HTMLElement> = T & { ref?: U | null };

type FlyAndScaleParams = {
	y?: number;
	x?: number;
	start?: number;
	duration?: number;
};

export const flyAndScale = (
	node: Element,
	params: FlyAndScaleParams = { y: -8, x: 0, start: 0.95, duration: 150 }
): TransitionConfig => {
	const style = getComputedStyle(node);
	const transform = style.transform === "none" ? "" : style.transform;

	const scaleConversion = (
		valueA: number,
		scaleA: [number, number],
		scaleB: [number, number]
	) => {
		const [minA, maxA] = scaleA;
		const [minB, maxB] = scaleB;

		const percentage = (valueA - minA) / (maxA - minA);
		const valueB = percentage * (maxB - minB) + minB;

		return valueB;
	};

	const styleToString = (
		style: Record<string, number | string | undefined>
	): string => {
		return Object.keys(style).reduce((str, key) => {
			if (style[key] === undefined) return str;
			return str + `${key}:${style[key]};`;
		}, "");
	};

	return {
		duration: params.duration ?? 200,
		delay: 0,
		css: (t) => {
			const y = scaleConversion(t, [0, 1], [params.y ?? 5, 0]);
			const x = scaleConversion(t, [0, 1], [params.x ?? 0, 0]);
			const scale = scaleConversion(t, [0, 1], [params.start ?? 0.95, 1]);

			return styleToString({
				transform: `${transform} translate3d(${x}px, ${y}px, 0) scale(${scale})`,
				opacity: t
			});
		},
		easing: cubicOut
	};
};

export const preventEvent = (e: DragEvent) => {
	e.preventDefault();
	e.stopPropagation();
};

export const formatSize = (size: number | null): string => {
	if (size === null) return "0 B";

	if (size < 1024) {
		return `${size} B`;
	}
	if (size < 1024 * 1024) {
		const kbSize = size / 1024;
		return kbSize < 1000 ? `${kbSize.toFixed(2)} KB` : `${(kbSize / 1024).toFixed(2)} MB`;
	}
	if (size < 1024 * 1024 * 1024) {
		const mbSize = size / (1024 * 1024);
		return mbSize < 1000 ? `${mbSize.toFixed(2)} MB` : `${(mbSize / 1024).toFixed(2)} GB`;
	}
	if (size < 1024 * 1024 * 1024 * 1024) {
		const gbSize = size / (1024 * 1024 * 1024);
		return gbSize < 1000 ? `${gbSize.toFixed(2)} GB` : `${(gbSize / 1024).toFixed(2)} TB`;
	}

	return `${(size / (1024 * 1024 * 1024 * 1024)).toFixed(2)} TB`;
};

export const formatDate = (date: Date) => {
	return format(date, "PPPp", {
		locale: (() => {
			const lang = window.navigator.language;
			if (lang.startsWith("ko")) return ko;
			if (lang.startsWith("zh")) return zhCN;
			return enUS;
		})(),
	})
}

export function isNameConflict(childs: { name: string }[], name: string) {
	return childs.some(child => child.name === name);
}