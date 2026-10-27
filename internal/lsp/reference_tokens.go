package lsp

import (
	"strings"
	"unicode/utf8"

	"geblang/internal/lexer"
	"geblang/internal/token"
)

func referenceTokens(source string) []token.Token {
	l := lexer.New(source)
	var tokens []token.Token
	for tok := l.NextToken(); tok.Type != token.EOF; tok = l.NextToken() {
		tokens = append(tokens, tok)
	}
	var out []token.Token
	for i, tok := range tokens {
		label := i+1 < len(tokens) && tokens[i+1].Type == token.Colon && (i == 0 || tokens[i-1].Type == token.LParen || tokens[i-1].Type == token.Comma || tokens[i-1].Type == token.LBrace)
		if tok.Type == token.Ident && !label {
			out = append(out, tok)
		}
		if tok.Type == token.String && tok.Quote == '"' {
			out = append(out, interpolationTokens(tok)...)
		}
	}
	return out
}

func interpolationTokens(str token.Token) []token.Token {
	var out []token.Token
	for offset := 0; offset < len(str.Raw); {
		start := strings.Index(str.Raw[offset:], "${")
		if start < 0 {
			break
		}
		start += offset + 2
		text := str.Raw[start:]
		l := lexer.New(text)
		braces, parens, brackets, ternaries := 1, 0, 0, 0
		end, close := -1, -1
		for tok := l.NextToken(); tok.Type != token.EOF; tok = l.NextToken() {
			switch tok.Type {
			case token.LBrace:
				braces++
			case token.RBrace:
				braces--
			case token.LParen:
				parens++
			case token.RParen:
				parens--
			case token.LBracket:
				brackets++
			case token.RBracket:
				brackets--
			case token.Question:
				if braces == 1 && parens == 0 && brackets == 0 {
					ternaries++
				}
			case token.Colon:
				if braces == 1 && parens == 0 && brackets == 0 {
					if ternaries > 0 {
						ternaries--
					} else if end < 0 {
						end = sourceTokenOffset(text, tok)
					}
				}
			}
			if braces == 0 {
				close = sourceTokenOffset(text, tok)
				break
			}
		}
		if close < 0 {
			break
		}
		if end < 0 {
			end = close
		}
		prefix := str.Raw[:start]
		line := str.Line + strings.Count(prefix, "\n")
		column := str.Column + 1 + utf8.RuneCountInString(prefix)
		if i := strings.LastIndexByte(prefix, '\n'); i >= 0 {
			column = 1 + utf8.RuneCountInString(prefix[i+1:])
		}
		for _, tok := range referenceTokens(text[:end]) {
			if tok.Line == 1 {
				tok.Column += column - 1
			}
			tok.Line += line - 1
			out = append(out, tok)
		}
		offset = start + close + 1
	}
	return out
}

func sourceTokenOffset(source string, tok token.Token) int {
	start := 0
	for line := 1; line < tok.Line; line++ {
		i := strings.IndexByte(source[start:], '\n')
		if i < 0 {
			return len(source)
		}
		start += i + 1
	}
	return start + byteOffset(splitLine(source, tok.Line-1), tokenColumn(splitLine(source, tok.Line-1), tok.Column-1))
}
