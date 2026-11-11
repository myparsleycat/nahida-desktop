package ui

import (
	"strings"
	"unicode/utf8"
)

// Hangul syllables decompose into an initial consonant, a vowel and an optional final consonant
// (Unicode 3.12). The jamo are the compatibility letters a keyboard types.
const (
	hangulBase  = 0xAC00
	hangulLast  = 0xD7A3
	vowelCount  = 21
	finalCount  = 28
	initialJamo = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ"
	vowelJamo   = "ㅏㅐㅑㅒㅓㅔㅕㅖㅗㅘㅙㅚㅛㅜㅝㅞㅟㅠㅡㅢㅣ"
	finalJamo   = " ㄱㄲㄳㄴㄵㄶㄷㄹㄺㄻㄼㄽㄾㄿㅀㅁㅂㅄㅅㅆㅇㅈㅊㅋㅌㅍㅎ"
)

var (
	initials = []rune(initialJamo)
	vowels   = []rune(vowelJamo)
	finals   = []rune(finalJamo)
)

// compoundJamo splits the letters typed as two keys, so a partly typed syllable still matches.
var compoundJamo = map[rune]string{
	'ㄳ': "ㄱㅅ", 'ㄵ': "ㄴㅈ", 'ㄶ': "ㄴㅎ", 'ㄺ': "ㄹㄱ", 'ㄻ': "ㄹㅁ", 'ㄼ': "ㄹㅂ", 'ㄽ': "ㄹㅅ", 'ㄾ': "ㄹㅌ",
	'ㄿ': "ㄹㅍ", 'ㅀ': "ㄹㅎ", 'ㅄ': "ㅂㅅ", 'ㅘ': "ㅗㅏ", 'ㅙ': "ㅗㅐ", 'ㅚ': "ㅗㅣ", 'ㅝ': "ㅜㅓ", 'ㅞ': "ㅜㅔ",
	'ㅟ': "ㅜㅣ", 'ㅢ': "ㅡㅣ",
}

// similarSounds are the letters a search also accepts in place of the typed one.
var similarSounds = map[rune][]rune{
	'ㄱ': {'ㄲ', 'ㅋ'}, 'ㄷ': {'ㄸ', 'ㅌ'}, 'ㅂ': {'ㅃ', 'ㅍ'}, 'ㅅ': {'ㅆ'}, 'ㅈ': {'ㅉ', 'ㅊ'}, 'ㅐ': {'ㅔ'}, 'ㅔ': {'ㅐ'},
}

// disassemble writes text as single jamo, as es-hangul's disassemble does.
func disassemble(text string) string {
	var b strings.Builder
	b.Grow(len(text) * 2)
	put := func(r rune) {
		if split, ok := compoundJamo[r]; ok {
			b.WriteString(split)
			return
		}
		b.WriteRune(r)
	}
	for _, r := range text {
		if r < hangulBase || r > hangulLast {
			put(r)
			continue
		}
		index := int(r - hangulBase)
		put(initials[index/(vowelCount*finalCount)])
		put(vowels[index%(vowelCount*finalCount)/finalCount])
		if final := index % finalCount; final > 0 {
			put(finals[final])
		}
	}
	return b.String()
}

// choseong keeps the initial consonant of each syllable and the spaces between words, as es-hangul's getChoseong
// does.
func choseong(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= hangulBase && r <= hangulLast:
			b.WriteRune(initials[int(r-hangulBase)/(vowelCount*finalCount)])
		case r == ' ' || strings.ContainsRune(initialJamo, r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// searchIndex holds the forms of a name a search compares against.
type searchIndex struct {
	lower, jamo, chosung string
}

func newSearchIndex(name string) searchIndex {
	lower := strings.ToLower(name)
	return searchIndex{lower: lower, jamo: disassemble(lower), chosung: choseong(lower)}
}

// searchScore ranks how well name matches query, 0 for no match. It follows the React screen's sejong search:
// exact, prefix and substring matches first, then initial consonants, partly typed syllables and similar
// sounding letters. query must be lower case.
func searchScore(index searchIndex, query string) int {
	switch {
	case index.lower == query:
		return 100
	case strings.HasPrefix(index.lower, query):
		return 90
	case strings.Contains(index.lower, query):
		return 80
	}
	queryJamo := disassemble(query)
	if strings.Contains(disassemble(index.chosung), queryJamo) {
		return 75
	}
	if strings.Contains(index.jamo, queryJamo) {
		return 70
	}
	for i, r := range query {
		for _, similar := range similarSounds[r] {
			if strings.Contains(index.lower, query[:i]+string(similar)+query[i+utf8.RuneLen(r):]) {
				return 60
			}
		}
	}
	return 0
}
