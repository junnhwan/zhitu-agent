package memory

import "unicode"

func EstimateTokens(text string) int {
	cjkRunes := 0
	otherBytes := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			cjkRunes++
		} else {
			otherBytes += len(string(r))
		}
	}
	return cjkRunes/2 + otherBytes/4
}
