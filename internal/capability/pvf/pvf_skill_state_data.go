package pvf

import (
	"strconv"
	"strings"

	"robot/internal/shared"
)

type squirrelToken struct {
	text string
	line int
}

// Tokenize only for source metadata, never to execute Squirrel. Keep strings
// opaque and discard comments so sample code cannot become a declaration/call.
func squirrelTokens(text string) ([]squirrelToken, bool) {
	text = strings.TrimPrefix(text, "\ufeff")
	var tokens []squirrelToken
	line := 1
	for i := 0; i < len(text); {
		start, startLine := i, line
		switch {
		case isPVFSpace(text[i]):
			if text[i] == '\n' {
				line++
			}
			i++
			continue
		case strings.HasPrefix(text[i:], "//"):
			for i < len(text) && text[i] != '\n' {
				i++
			}
			continue
		case strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
			line += strings.Count(text[start:i], "\n")
			continue
		case text[i] == '"' || text[i] == '\'' || strings.HasPrefix(text[i:], "@\""):
			verbatim := text[i] == '@'
			if verbatim {
				i++
			}
			quote := text[i]
			i++
			closed := false
			for i < len(text) {
				if text[i] == '\n' {
					line++
				}
				if !verbatim && text[i] == '\\' && i+1 < len(text) {
					if text[i+1] == '\n' {
						line++
					}
					i += 2
					continue
				}
				if text[i] == quote {
					i++
					if verbatim && i < len(text) && text[i] == quote {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, false
			}
		case squirrelWordByte(text[i]):
			for i < len(text) && squirrelWordByte(text[i]) {
				i++
			}
		case strings.HasPrefix(text[i:], "<-"):
			i += 2
		default:
			i++
		}
		tokens = append(tokens, squirrelToken{text[start:i], startLine})
	}
	return tokens, true
}

func squirrelWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func skillSymbolName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func squirrelInt(tokens []squirrelToken) (int, bool) {
	if len(tokens) == 1 && skillSymbolName(tokens[0].text) {
		return 0, false
	}
	var value string
	if len(tokens) == 1 {
		value = tokens[0].text
	} else if len(tokens) == 2 && (tokens[0].text == "-" || tokens[0].text == "+") {
		value = tokens[0].text + tokens[1].text
	} else {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(strings.TrimLeft(value, "+-"), "0x") || strings.HasPrefix(strings.TrimLeft(value, "+-"), "0X") {
		base = 0
	}
	n, err := strconv.ParseInt(value, base, 64)
	return int(n), err == nil && n >= -2147483648 && n <= 2147483647
}

func squirrelStateValue(tokens []squirrelToken, symbols map[string]int) (int, bool) {
	if len(tokens) == 1 {
		if n, ok := symbols[tokens[0].text]; ok {
			return n, true
		}
	}
	return squirrelInt(tokens)
}

func squirrelAssignment(tokens []squirrelToken, i int) bool {
	if i+1 >= len(tokens) {
		return false
	}
	next := tokens[i+1].text
	if next == "<-" || next == "=" && (i+2 == len(tokens) || tokens[i+2].text != "=") {
		return true
	}
	return i+2 < len(tokens) && strings.Contains("+-*/%&|^", next) &&
		(tokens[i+2].text == "=" || tokens[i+2].text == next && (next == "+" || next == "-"))
}

// Only root integer declarations are metadata. Conflicting definitions and
// runtime assignments stay unknown, independent of map iteration order.
func collectSkillSymbols(scripts map[string]string, symbols map[string]int) {
	ambiguous := make(map[string]bool)
	for _, text := range scripts {
		tokens, ok := squirrelTokens(text)
		if !ok {
			continue
		}
		depth := 0
		for i, token := range tokens {
			if token.text == "{" {
				depth++
			} else if token.text == "}" {
				depth--
			}
			if !skillSymbolName(token.text) || !squirrelAssignment(tokens, i) {
				continue
			}
			end := i + 3
			if i+2 < len(tokens) && (tokens[i+2].text == "-" || tokens[i+2].text == "+") {
				end++
			}
			value, valid := 0, false
			if depth == 0 && end <= len(tokens) && (tokens[i+1].text == "<-" || tokens[i+1].text == "=") {
				value, valid = squirrelInt(tokens[i+2 : end])
				if end < len(tokens) && tokens[end].text != ";" {
					valid = valid && tokens[end].line > tokens[end-1].line &&
						(tokens[end].text == "function" || tokens[end].text == "const" ||
							skillSymbolName(tokens[end].text) && end+1 < len(tokens) &&
								(tokens[end+1].text == "<-" || tokens[end+1].text == "="))
				}
			}
			previous, exists := symbols[token.text]
			if !valid || exists && previous != value {
				ambiguous[token.text] = true
			} else {
				symbols[token.text] = value
			}
		}
	}
	for name := range ambiguous {
		delete(symbols, name)
	}
}

// Read a single receiver.method(...) statement. Nested argument expressions
// remain tokens and cannot be mistaken for a literal parameter.
func squirrelMethod(tokens []squirrelToken, i int, receiver string) (string, [][]squirrelToken, int) {
	if i+4 >= len(tokens) || tokens[i].text != receiver || tokens[i+1].text != "." || tokens[i+3].text != "(" {
		return "", nil, i
	}
	var args [][]squirrelToken
	start, depth := i+4, 0
	for j := start; j < len(tokens); j++ {
		switch tokens[j].text {
		case "(", "[", "{":
			depth++
		case ")":
			if depth == 0 {
				if j > start {
					args = append(args, tokens[start:j])
				}
				if j+1 < len(tokens) && tokens[j+1].text == ";" {
					return tokens[i+2].text, args, j + 2
				}
				return "", nil, i
			}
			depth--
		case "]", "}":
			depth--
		case ",":
			if depth == 0 {
				args = append(args, tokens[start:j])
				start = j + 1
			}
		}
		if depth < 0 {
			break
		}
	}
	return "", nil, i
}

// Extract a contiguous clear/push/send sequence from the registered entry
// function. This is only its initial wire vector, not proof of eligibility or
// an implementation of its checks, state machine, targeting or damage logic.
func scriptEntryStateData(text, suffix string, state int, symbols map[string]int) []int {
	tokens, ok := squirrelTokens(text)
	if !ok || suffix == "" {
		return nil
	}
	var body []squirrelToken
	receiver := ""
	depth := 0
	for i, token := range tokens {
		if depth == 0 && token.text == "function" && i+6 < len(tokens) && tokens[i+1].text == "checkExecutableSkill_"+suffix &&
			tokens[i+2].text == "(" && tokens[i+4].text == ")" && tokens[i+5].text == "{" {
			if body != nil {
				return nil
			}
			receiver = tokens[i+3].text
			level := 1
			for j := i + 6; j < len(tokens); j++ {
				if tokens[j].text == "{" {
					level++
				} else if tokens[j].text == "}" {
					level--
				}
				if level == 0 {
					body = tokens[i+6 : j]
					break
				}
			}
		}
		if token.text == "{" {
			depth++
		} else if token.text == "}" {
			depth--
		}
	}
	sends := 0
	for i, token := range body {
		if token.text == "sq_AddSetStatePacket" {
			sends++
		}
		if token.text == "function" || token.text == receiver && squirrelAssignment(body, i) {
			return nil
		}
		// A local can shadow a root constant, including an uninitialized local.
		// Exclude every symbol in its declaration rather than evaluate it.
		if token.text == "local" {
			level, declaration := 0, true
			for j := i + 1; j < len(body) && body[j].text != ";"; j++ {
				if declaration {
					if _, exists := symbols[body[j].text]; exists {
						return nil
					}
					declaration = false
				}
				switch body[j].text {
				case "(", "[", "{":
					level++
				case ")", "]", "}":
					level--
				case ",":
					declaration = level == 0
				}
			}
		}
	}
	if sends != 1 {
		return nil
	}
	for i := range body {
		// A naked `if (condition) clear();` does not establish the vector for
		// following statements. The entire sequence must share a block.
		if i > 0 && body[i-1].text != "{" && body[i-1].text != ";" {
			continue
		}
		name, args, end := squirrelMethod(body, i, receiver)
		if name != "sq_IntVectClear" || len(args) != 0 {
			continue
		}
		values := make([]int, 0)
		for end < len(body) {
			name, args, next := squirrelMethod(body, end, receiver)
			if name == "sq_IntVectPush" && len(args) == 1 && len(values) < shared.MaxPartySkillStateDataSize {
				value, valid := squirrelStateValue(args[0], symbols)
				if !valid {
					break
				}
				values = append(values, value)
				end = next
				continue
			}
			if name == "sq_AddSetStatePacket" && len(args) == 3 {
				if target, valid := squirrelStateValue(args[0], symbols); valid && target == state {
					return values
				}
			}
			break
		}
	}
	return nil
}
