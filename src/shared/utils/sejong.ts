class SejongClass {
    private readonly CHOSUNG = [
        "ㄱ", "ㄲ", "ㄴ", "ㄷ", "ㄸ",
        "ㄹ", "ㅁ", "ㅂ", "ㅃ", "ㅅ",
        "ㅆ", "ㅇ", "ㅈ", "ㅉ", "ㅊ",
        "ㅋ", "ㅌ", "ㅍ", "ㅎ"
    ];
    private readonly JUNGSUNG = "ㅏㅐㅑㅒㅓㅔㅕㅖㅗㅘㅙㅚㅛㅜㅝㅞㅟㅠㅡㅢㅣ";
    private readonly JONGSUNG = " ㄱㄲㄳㄴㄵㄶㄷㄹㄺㄻㄼㄽㄾㄿㅀㅁㅂㅄㅅㅆㅇㅈㅊㅋㅌㅍㅎ";
    private readonly similarSoundMap = new Map([
        ['ㄱ', ['ㄲ', 'ㅋ']],
        ['ㄷ', ['ㄸ', 'ㅌ']],
        ['ㅂ', ['ㅃ', 'ㅍ']],
        ['ㅅ', ['ㅆ']],
        ['ㅈ', ['ㅉ', 'ㅊ']],
        ['ㅐ', ['ㅔ']],
        ['ㅔ', ['ㅐ']],
    ]);

    public getChosung(str: string) {
        let result = "";

        for (let i = 0; i < str.length; i++) {
            const code = str.charCodeAt(i);
            if (code >= 44032 && code <= 55203) {
                result += this.CHOSUNG[Math.floor((code - 44032) / 588)];
            } else {
                result += str[i];
            }
        }
        return result;
    }

    private decomposeHangul(str: string) {
        const result: string[] = [];

        for (let i = 0; i < str.length; i++) {
            const char = str.charAt(i);
            const code = char.charCodeAt(0);

            if (code >= 44032 && code <= 55203) {
                const charCode = code - 44032;
                const cho = Math.floor(charCode / 588);
                const jung = Math.floor((charCode % 588) / 28);
                const jong = charCode % 28;

                result.push(this.CHOSUNG[cho]);
                result.push(this.JUNGSUNG[jung]);
                if (jong !== 0) result.push(this.JONGSUNG[jong]);
            } else {
                result.push(char);
            }
        }

        return result;
    }

    private getSimilarPatterns(str: string) {
        const patterns: string[] = [str];

        for (let i = 0; i < str.length; i++) {
            const char = str[i];
            const similarChars = this.similarSoundMap.get(char);

            if (similarChars) {
                similarChars.forEach(similarChar => {
                    patterns.push(str.slice(0, i) + similarChar + str.slice(i + 1));
                });
            }
        }

        return patterns;
    }

    private isPartialMatch(target: string, query: string) {
        const decomposedTarget = this.decomposeHangul(target.toLowerCase());
        const decomposedQuery = this.decomposeHangul(query.toLowerCase());

        const targetJamo = decomposedTarget.join('');
        const queryJamo = decomposedQuery.join('');

        return targetJamo.includes(queryJamo);
    }

    public getSearchScore(itemName: string, query: string) {
        const lowerItemName = itemName.toLowerCase();
        const lowerQuery = query.toLowerCase();

        if (lowerItemName === lowerQuery) return 100;
        if (lowerItemName.startsWith(lowerQuery)) return 90;
        if (lowerItemName.includes(lowerQuery)) return 80;
        if (this.isPartialMatch(itemName, query)) return 70;

        const similarPatterns = this.getSimilarPatterns(lowerQuery);
        if (similarPatterns.some(pattern => lowerItemName.includes(pattern))) return 60;

        return 0;
    }

    public search<T>(
        items: T[],
        query: string,
        getItemName: (item: T) => string,
        minScore: number = 1
    ): Array<T & { searchScore: number }> {
        if (!query.trim()) return [];

        return items
            .map(item => ({
                ...item,
                searchScore: this.getSearchScore(getItemName(item), query)
            }))
            .filter(item => item.searchScore >= minScore)
            .sort((a, b) => b.searchScore - a.searchScore);
    }

    public isChosungMatch(itemName: string, query: string): boolean {
        const itemChosung = this.getChosung(itemName);
        return itemChosung.includes(query);
    }
}

export const Sejong = new SejongClass();