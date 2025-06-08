export const getRandInt = (min: number, max: number) => {
    return Math.floor(Math.random() * (max - min + 1)) + min;
}

export function delay(ms: number) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

interface RandomStringOptions {
    includeUppercase?: boolean;
    includeLowercase?: boolean;
    includeNumbers?: boolean;
    includeHyphen?: boolean;
    includeDash?: boolean;
}

export function GenerateRandomString(length: number, options: RandomStringOptions = {}): string {
    const defaultOptions: RandomStringOptions = {
        includeUppercase: true,
        includeLowercase: true,
        includeNumbers: true,
        includeHyphen: false,
        includeDash: false
    };

    const config = { ...defaultOptions, ...options };

    let characters = '';

    if (config.includeUppercase) {
        characters += 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
    }

    if (config.includeLowercase) {
        characters += 'abcdefghijklmnopqrstuvwxyz';
    }

    if (config.includeNumbers) {
        characters += '0123456789';
    }

    if (config.includeHyphen) {
        characters += '-';
    }

    if (config.includeDash) {
        characters += '_';
    }

    if (characters === '') {
        throw new Error('최소 하나의 문자 타입은 포함되어야 함');
    }

    let result: string = '';
    for (let i = 0; i < length; i++) {
        const randomIndex: number = Math.floor(Math.random() * characters.length);
        result += characters[randomIndex];
    }

    return result;
}