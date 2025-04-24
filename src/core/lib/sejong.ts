// 초성 추출
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

// 자모 분리
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

// 부분 일치 검사
const isPartialMatch = (target: string, query: string): boolean => {
  const decomposedTarget = decomposeHangul(target.toLowerCase());
  const decomposedQuery = decomposeHangul(query.toLowerCase());

  const targetJamo = decomposedTarget.join('');
  const queryJamo = decomposedQuery.join('');

  // 자모 단위 부분 일치 검사
  return targetJamo.includes(queryJamo);
};

// 검색 점수 계산 
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