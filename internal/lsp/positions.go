package lsp

import "unicode/utf8"

func byteOffset(text string, character int) int {
	units := 0
	for i, r := range text {
		if units >= character {
			return i
		}
		units++
		if r > 0xffff {
			units++
		}
	}
	return len(text)
}

func utf16Offset(text string, offset int) int {
	units := 0
	for i, r := range text {
		if i >= offset {
			break
		}
		units++
		if r > 0xffff {
			units++
		}
	}
	return units
}

func runeColumn(text string, character int) int {
	return utf8.RuneCountInString(text[:byteOffset(text, character)])
}

func tokenColumn(text string, column int) int {
	count := 0
	for i := range text {
		if count == column {
			return utf16Offset(text, i)
		}
		count++
	}
	return utf16Offset(text, len(text))
}
