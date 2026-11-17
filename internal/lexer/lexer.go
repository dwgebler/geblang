package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"geblang/internal/token"
)

type Lexer struct {
	input        string
	position     int
	readPosition int
	ch           rune
	line         int
	column       int
	pendingDocs  []string
	comments     []Comment
}

// Comment is a source comment captured for the formatter (the parser/AST ignore these). Kind is "line", "doc-line", "block", or "doc-block".
type Comment struct {
	Kind string
	Text string
	Line int
}

// Comments returns every comment lexed so far, in source order.
func (l *Lexer) Comments() []Comment { return l.comments }

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1}
	l.readChar()
	return l
}

// NewAt lexes input as if it began on the given line, for source embedded in a larger file.
func NewAt(input string, line int) *Lexer {
	l := &Lexer{input: input, line: line}
	l.readChar()
	return l
}

func (l *Lexer) NextToken() token.Token {
	l.skipIgnored()

	tok := token.Token{Line: l.line, Column: l.column}
	switch l.ch {
	case 0:
		tok.Type = token.EOF
	case '=':
		tok = l.matchToken('=', token.Eq, token.Assign)
		if tok.Type == token.Assign && l.peekChar() == '>' {
			l.readChar()
			tok.Type = token.Arrow
			tok.Literal = "=>"
			tok.Raw = tok.Literal
		}
	case '+':
		tok = l.matchToken('+', token.Inc, token.Plus)
		if tok.Type == token.Plus && l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.PlusAssign
			tok.Literal = "+="
			tok.Raw = "+="
		}
	case '-':
		tok = l.matchToken('-', token.Dec, token.Minus)
		if tok.Type == token.Minus && l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.MinusAssign
			tok.Literal = "-="
			tok.Raw = "-="
		}
	case '!':
		tok = l.matchToken('=', token.NotEq, token.Bang)
	case '*':
		tok = l.matchToken('*', token.Power, token.Asterisk)
		if l.peekChar() == '=' {
			l.readChar()
			switch tok.Type {
			case token.Power:
				tok.Type = token.PowerAssign
				tok.Literal = "**="
				tok.Raw = "**="
			default:
				tok.Type = token.MulAssign
				tok.Literal = "*="
				tok.Raw = "*="
			}
		}
	case '/':
		tok = l.matchToken('/', token.IntDiv, token.Slash)
		if l.peekChar() == '=' {
			l.readChar()
			switch tok.Type {
			case token.IntDiv:
				tok.Type = token.IntDivAssign
				tok.Literal = "//="
				tok.Raw = "//="
			default:
				tok.Type = token.DivAssign
				tok.Literal = "/="
				tok.Raw = "/="
			}
		}
	case '%':
		tok = l.newToken(token.Percent)
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.ModAssign
			tok.Literal = "%="
			tok.Raw = "%="
		}
	case '<':
		switch l.peekChar() {
		case '=':
			tok = l.twoCharToken(token.LTE)
		case '<':
			tok = l.twoCharToken(token.LShift)
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = token.LShiftAssign
				tok.Literal = "<<="
				tok.Raw = "<<="
			}
		default:
			tok = l.newToken(token.LT)
		}
	case '>':
		switch l.peekChar() {
		case '=':
			tok = l.twoCharToken(token.GTE)
		case '>':
			tok = l.twoCharToken(token.RShift)
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = token.RShiftAssign
				tok.Literal = ">>="
				tok.Raw = ">>="
			}
		default:
			tok = l.newToken(token.GT)
		}
	case '&':
		tok = l.matchToken('&', token.And, token.BitAnd)
		if tok.Type == token.BitAnd && l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.BitAndAssign
			tok.Literal = "&="
			tok.Raw = "&="
		}
	case '|':
		tok = l.matchToken('|', token.Or, token.BitOr)
		if tok.Type == token.BitOr && l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.BitOrAssign
			tok.Literal = "|="
			tok.Raw = "|="
		} else if tok.Type == token.BitOr && l.peekChar() == '>' {
			l.readChar()
			tok.Type = token.Pipe
			tok.Literal = "|>"
			tok.Raw = "|>"
		}
	case '^':
		tok = l.newToken(token.BitXor)
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.BitXorAssign
			tok.Literal = "^="
			tok.Raw = "^="
		}
	case '~':
		tok = l.newToken(token.BitNot)
	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			if l.peekChar() == '.' {
				l.readChar()
				tok = token.Token{Type: token.Ellipsis, Literal: "...", Raw: "...", Line: tok.Line, Column: tok.Column}
			} else {
				tok = token.Token{Type: token.Range, Literal: "..", Raw: "..", Line: tok.Line, Column: tok.Column}
				if l.peekChar() == '<' {
					l.readChar()
					tok.Type = token.RangeExcl
					tok.Literal = "..<"
					tok.Raw = tok.Literal
				}
			}
		} else {
			tok = l.newToken(token.Dot)
		}
	case ',':
		tok = l.newToken(token.Comma)
	case ':':
		tok = l.newToken(token.Colon)
	case ';':
		tok = l.newToken(token.Semicolon)
	case '?':
		switch l.peekChar() {
		case '?':
			tok = l.twoCharToken(token.NullCoalesce)
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = token.NullCoalesceAssign
				tok.Literal = "??="
				tok.Raw = "??="
			}
		case '.':
			tok = l.twoCharToken(token.OptionalChain)
		default:
			tok = l.newToken(token.Question)
		}
	case '@':
		tok = l.newToken(token.At)
	case '(':
		tok = l.newToken(token.LParen)
	case ')':
		tok = l.newToken(token.RParen)
	case '{':
		tok = l.newToken(token.LBrace)
	case '}':
		tok = l.newToken(token.RBrace)
	case '[':
		tok = l.newToken(token.LBracket)
	case ']':
		tok = l.newToken(token.RBracket)
	case '"', '\'':
		return l.readStringToken(l.ch)
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			l.attachDoc(&tok)
			return tok
		}
		if isDigit(l.ch) {
			tok := l.readNumberToken()
			l.attachDoc(&tok)
			return tok
		}
		tok = l.newToken(token.Illegal)
	}

	l.attachDoc(&tok)
	l.readChar()
	return tok
}

func (l *Lexer) attachDoc(tok *token.Token) {
	if len(l.pendingDocs) == 0 {
		return
	}
	tok.Doc = strings.Join(l.pendingDocs, "\n")
	l.pendingDocs = nil
}

func (l *Lexer) matchToken(next rune, two token.Type, one token.Type) token.Token {
	if l.peekChar() == next {
		return l.twoCharToken(two)
	}
	return l.newToken(one)
}

func (l *Lexer) twoCharToken(tokenType token.Type) token.Token {
	line, column := l.line, l.column
	ch := l.ch
	l.readChar()
	lit := string(ch) + string(l.ch)
	return token.Token{Type: tokenType, Literal: lit, Raw: lit, Line: line, Column: column}
}

func (l *Lexer) newToken(tokenType token.Type) token.Token {
	return token.Token{Type: tokenType, Literal: string(l.ch), Raw: string(l.ch), Line: l.line, Column: l.column}
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
		l.position = l.readPosition
		return
	} else {
		ch, width := utf8.DecodeRuneInString(l.input[l.readPosition:])
		l.ch = ch
		l.position = l.readPosition
		l.readPosition += width
	}

	if l.ch == '\n' {
		l.line++
		l.column = 0
	} else {
		l.column++
	}
}

func (l *Lexer) peekChar() rune {
	if l.readPosition >= len(l.input) {
		return 0
	}
	ch, _ := utf8.DecodeRuneInString(l.input[l.readPosition:])
	return ch
}

func (l *Lexer) peekSecondChar() rune {
	if l.readPosition >= len(l.input) {
		return 0
	}
	_, width := utf8.DecodeRuneInString(l.input[l.readPosition:])
	nextPosition := l.readPosition + width
	if nextPosition >= len(l.input) {
		return 0
	}
	ch, _ := utf8.DecodeRuneInString(l.input[nextPosition:])
	return ch
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumberToken() token.Token {
	line, column := l.line, l.column
	position := l.position
	tokenType := token.Int

	if l.ch == '0' && isBasePrefix(l.peekChar()) {
		l.readChar()
		l.readChar()
		for isBaseLiteralPart(l.ch) {
			l.readChar()
		}
		lit := l.input[position:l.position]
		return token.Token{Type: token.Int, Literal: lit, Raw: lit, Line: line, Column: column}
	}

	for isDigit(l.ch) || l.ch == '_' {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		tokenType = token.Decimal
		l.readChar()
		for isDigit(l.ch) || l.ch == '_' {
			l.readChar()
		}
	}
	if (l.ch == 'e' || l.ch == 'E') && (isDigit(l.peekChar()) || ((l.peekChar() == '+' || l.peekChar() == '-') && isDigit(l.peekSecondChar()))) {
		tokenType = token.Decimal
		l.readChar()
		if l.ch == '+' || l.ch == '-' {
			l.readChar()
		}
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	if l.ch == 'f' {
		tokenType = token.Float
		l.readChar()
	}

	lit := l.input[position:l.position]
	return token.Token{Type: tokenType, Literal: lit, Raw: lit, Line: line, Column: column}
}

func isBasePrefix(ch rune) bool {
	switch ch {
	case 'b', 'B', 'o', 'O', 'x', 'X':
		return true
	default:
		return false
	}
}

func isBaseLiteralPart(ch rune) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') || ch == '_'
}

func (l *Lexer) readStringToken(quote rune) token.Token {
	startLine, startColumn := l.line, l.column
	triple := l.peekChar() == quote && l.peekSecondChar() == quote
	if triple {
		l.readChar()
		l.readChar()
	}

	var raw strings.Builder
	var value strings.Builder
	interpolated := false

	for {
		l.readChar()
		if l.ch == 0 || (!triple && l.ch == '\n') {
			return token.Token{
				Type:    token.Illegal,
				Literal: fmt.Sprintf("unterminated string at %d:%d", startLine, startColumn),
				Line:    startLine,
				Column:  startColumn,
			}
		}
		if l.ch == quote {
			if !triple {
				break
			}
			if l.peekChar() == quote && l.peekSecondChar() == quote {
				l.readChar()
				l.readChar()
				break
			}
		}

		raw.WriteRune(l.ch)
		if quote == '"' && l.ch == '\\' {
			next := l.peekChar()
			if next == 0 {
				continue
			}
			l.readChar()
			raw.WriteRune(l.ch)
			if l.ch == 'u' && l.peekChar() == '{' {
				l.readChar()
				raw.WriteRune(l.ch)
				var hex strings.Builder
				for l.peekChar() != '}' && l.peekChar() != 0 {
					l.readChar()
					raw.WriteRune(l.ch)
					hex.WriteRune(l.ch)
				}
				if l.peekChar() != '}' {
					return token.Token{Type: token.Illegal, Literal: fmt.Sprintf("unterminated \\u{...} escape at %d:%d", startLine, startColumn), Line: startLine, Column: startColumn}
				}
				l.readChar()
				raw.WriteRune(l.ch)
				r, err := decodeUnicodeEscape(hex.String())
				if err != nil {
					return token.Token{Type: token.Illegal, Literal: fmt.Sprintf("%s at %d:%d", err.Error(), startLine, startColumn), Line: startLine, Column: startColumn}
				}
				value.WriteRune(r)
				continue
			}
			unquoted, err := strconv.Unquote(`"` + `\` + string(l.ch) + `"`)
			if err != nil {
				value.WriteByte('\\')
				value.WriteRune(l.ch)
			} else {
				value.WriteString(unquoted)
			}
			continue
		}
		if quote == '"' && l.ch == '$' && l.peekChar() == '{' {
			// '$' is already in raw (written above); scan the rest of ${...}
			interpolated = true
			l.readChar()        // advance to '{'
			raw.WriteRune(l.ch) // write '{'
			depth := 1
			for depth > 0 {
				l.readChar()
				if l.ch == 0 {
					break
				}
				raw.WriteRune(l.ch)
				if l.ch == '\'' || l.ch == '"' {
					// skip string literal so its braces don't affect depth
					strQ := l.ch
					for {
						l.readChar()
						if l.ch == 0 || l.ch == '\n' {
							break
						}
						raw.WriteRune(l.ch)
						if l.ch == '\\' {
							l.readChar()
							if l.ch != 0 {
								raw.WriteRune(l.ch)
							}
							continue
						}
						if l.ch == strQ {
							break
						}
					}
				} else if l.ch == '{' {
					depth++
				} else if l.ch == '}' {
					depth--
				}
			}
			continue
		}
		value.WriteRune(l.ch)
	}

	l.readChar()
	tok := token.Token{
		Type:         token.String,
		Literal:      value.String(),
		Raw:          raw.String(),
		Quote:        byte(quote),
		Triple:       triple,
		Interpolated: interpolated,
		Line:         startLine,
		Column:       startColumn,
	}
	l.attachDoc(&tok)
	return tok
}

// decodeUnicodeEscape decodes the hex body of a `\u{...}` escape. Shared by
// the lexer and the interpolation parser so both reject the same invalid
// (empty, out-of-range, or surrogate) codepoints rather than emit garbage.
func decodeUnicodeEscape(hex string) (rune, error) {
	if hex == "" {
		return 0, fmt.Errorf(`empty \u{} escape`)
	}
	code, err := strconv.ParseInt(hex, 16, 32)
	if err != nil || !utf8.ValidRune(rune(code)) {
		return 0, fmt.Errorf(`invalid unicode codepoint \u{%s}`, hex)
	}
	return rune(code), nil
}

// UnescapeDoubleQuoted decodes the escape sequences in a double-quoted
// string's literal content (no surrounding quotes, no `${...}`). The parser
// uses it on the literal segments of an interpolated string so they decode
// escapes identically to a plain string. Errors on a malformed `\u{...}`.
func UnescapeDoubleQuoted(content string) (string, error) {
	var out strings.Builder
	runes := []rune(content)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' || i+1 >= len(runes) {
			out.WriteRune(runes[i])
			continue
		}
		next := runes[i+1]
		if next == 'u' && i+2 < len(runes) && runes[i+2] == '{' {
			j := i + 3
			var hex strings.Builder
			for j < len(runes) && runes[j] != '}' {
				hex.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return "", fmt.Errorf(`unterminated \u{...} escape`)
			}
			r, err := decodeUnicodeEscape(hex.String())
			if err != nil {
				return "", err
			}
			out.WriteRune(r)
			i = j
			continue
		}
		if unquoted, err := strconv.Unquote(`"\` + string(next) + `"`); err == nil {
			out.WriteString(unquoted)
		} else {
			out.WriteByte('\\')
			out.WriteRune(next)
		}
		i++
	}
	return out.String(), nil
}

func (l *Lexer) skipIgnored() {
	for {
		for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
			l.readChar()
		}
		if l.ch == '#' {
			l.skipLineComment()
			continue
		}
		if l.ch == '/' && l.peekChar() == '*' {
			l.skipBlockComment()
			continue
		}
		return
	}
}

func (l *Lexer) skipLineComment() {
	startLine := l.line
	doc := false
	if l.peekChar() == '#' {
		doc = true
		l.readChar()
	}
	for l.peekChar() == ' ' || l.peekChar() == '\t' {
		l.readChar()
	}
	l.readChar() // step past the marker (or trailing space) to the first content char
	var text strings.Builder
	for l.ch != '\n' && l.ch != 0 {
		text.WriteRune(l.ch)
		l.readChar()
	}
	content := strings.TrimSpace(text.String())
	if doc {
		l.pendingDocs = append(l.pendingDocs, content)
		l.comments = append(l.comments, Comment{Kind: "doc-line", Text: content, Line: startLine})
	} else {
		l.comments = append(l.comments, Comment{Kind: "line", Text: content, Line: startLine})
	}
}

func (l *Lexer) skipBlockComment() {
	startLine := l.line
	l.readChar()
	l.readChar()
	doc := l.ch == '*'
	var text strings.Builder
	if doc {
		l.readChar()
	}
	for l.ch != 0 {
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar()
			l.readChar()
			break
		}
		text.WriteRune(l.ch)
		l.readChar()
	}
	raw := text.String()
	if doc {
		l.pendingDocs = append(l.pendingDocs, cleanBlockDoc(raw))
		l.comments = append(l.comments, Comment{Kind: "doc-block", Text: raw, Line: startLine})
	} else {
		l.comments = append(l.comments, Comment{Kind: "block", Text: raw, Line: startLine})
	}
}

func cleanBlockDoc(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "*") {
			line = strings.TrimLeft(line[1:], " \t")
		}
		lines[i] = line
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func isLetter(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}
