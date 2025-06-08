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
	if (size === null) return "";

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

// 초성 추출 함수
export const getChosung = (str: string): string => {
	const cho = ["ㄱ", "ㄲ", "ㄴ", "ㄷ", "ㄸ", "ㄹ", "ㅁ", "ㅂ", "ㅃ", "ㅅ", "ㅆ", "ㅇ", "ㅈ", "ㅉ", "ㅊ", "ㅋ", "ㅌ", "ㅍ", "ㅎ"];
	let result = "";

	for (let i = 0; i < str.length; i++) {
		const code = str.charCodeAt(i);
		if (code >= 44032 && code <= 55203) {
			result += cho[Math.floor((code - 44032) / 588)];
		} else {
			result += str[i];
		}
	}
	return result;
};

// 한글 자모 분리 함수
const decomposeHangul = (str: string): string[] => {
	const CHOSUNG = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ";
	const JUNGSUNG = "ㅏㅐㅑㅒㅓㅔㅕㅖㅗㅘㅙㅚㅛㅜㅝㅞㅟㅠㅡㅢㅣ";
	const JONGSUNG = " ㄱㄲㄳㄴㄵㄶㄷㄹㄺㄻㄼㄽㄾㄿㅀㅁㅂㅄㅅㅆㅇㅈㅊㅋㅌㅍㅎ";

	const result: string[] = [];

	for (let i = 0; i < str.length; i++) {
		const char = str.charAt(i);
		const code = char.charCodeAt(0);

		if (code >= 44032 && code <= 55203) {
			const charCode = code - 44032;
			const cho = Math.floor(charCode / 588);
			const jung = Math.floor((charCode % 588) / 28);
			const jong = charCode % 28;

			result.push(CHOSUNG[cho]);
			result.push(JUNGSUNG[jung]);
			if (jong !== 0) result.push(JONGSUNG[jong]);
		} else {
			result.push(char);
		}
	}

	return result;
};

// 유사 발음 매핑
const similarSoundMap = new Map([
	['ㄱ', ['ㄲ', 'ㅋ']],
	['ㄷ', ['ㄸ', 'ㅌ']],
	['ㅂ', ['ㅃ', 'ㅍ']],
	['ㅅ', ['ㅆ']],
	['ㅈ', ['ㅉ', 'ㅊ']],
	['ㅐ', ['ㅔ']],
	['ㅔ', ['ㅐ']],
]);

// 유사 발음 패턴 생성
const getSimilarPatterns = (str: string): string[] => {
	const patterns: string[] = [str];

	for (let i = 0; i < str.length; i++) {
		const char = str[i];
		const similarChars = similarSoundMap.get(char);

		if (similarChars) {
			similarChars.forEach(similarChar => {
				patterns.push(str.slice(0, i) + similarChar + str.slice(i + 1));
			});
		}
	}

	return patterns;
};

// 부분 일치 검사 함수
const isPartialMatch = (target: string, query: string): boolean => {
	const decomposedTarget = decomposeHangul(target.toLowerCase());
	const decomposedQuery = decomposeHangul(query.toLowerCase());

	const targetJamo = decomposedTarget.join('');
	const queryJamo = decomposedQuery.join('');

	// 자모 단위 부분 일치 검사
	return targetJamo.includes(queryJamo);
};

// 검색 점수 계산 함수
export const getSearchScore = (itemName: string, query: string): number => {
	const lowerItemName = itemName.toLowerCase();
	const lowerQuery = query.toLowerCase();

	// 정확한 매칭
	if (lowerItemName === lowerQuery) return 100;

	// 시작 부분 매칭
	if (lowerItemName.startsWith(lowerQuery)) return 90;

	// 단어 중간 정확한 매칭
	if (lowerItemName.includes(lowerQuery)) return 80;

	// 자모 분리 후 부분 매칭
	if (isPartialMatch(itemName, query)) return 70;

	// 유사 발음 매칭
	const similarPatterns = getSimilarPatterns(lowerQuery);
	if (similarPatterns.some(pattern => lowerItemName.includes(pattern))) return 60;

	return 0;
};