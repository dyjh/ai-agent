package shell

import "strings"

// ParseStructure performs shell structure analysis without executing the command.
func ParseStructure(command string) CommandStructure {
	tokens := tokenize(command)
	var (
		segments        []CommandSegment
		current         []string
		currentRedirect []string
		redirectTargets []string
		fileTargets     []string
		hasPipe         bool
		hasWriteRedir   bool
	)

	flush := func() {
		if len(current) == 0 {
			return
		}
		segment := CommandSegment{
			Raw:       strings.Join(current, " "),
			Name:      current[0],
			Redirects: append([]string(nil), currentRedirect...),
		}
		if len(current) > 1 {
			segment.Args = append([]string(nil), current[1:]...)
		}
		segments = append(segments, segment)
		current = nil
		currentRedirect = nil
	}

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token {
		case "|":
			hasPipe = true
			flush()
		case ">", ">>":
			hasWriteRedir = true
			currentRedirect = append(currentRedirect, token)
			if i+1 < len(tokens) {
				target := tokens[i+1]
				redirectTargets = append(redirectTargets, target)
				fileTargets = append(fileTargets, target)
			}
		default:
			if token == "<" {
				currentRedirect = append(currentRedirect, token)
				if i+1 < len(tokens) {
					fileTargets = append(fileTargets, tokens[i+1])
				}
				continue
			}
			current = append(current, token)
			if isPathLike(token) {
				fileTargets = append(fileTargets, token)
			}
		}
	}
	flush()

	return CommandStructure{
		Command:             command,
		Segments:            segments,
		HasPipeline:         hasPipe,
		HasWriteRedirect:    hasWriteRedir,
		RedirectTargets:     uniqStrings(redirectTargets),
		PossibleFileTargets: uniqStrings(fileTargets),
	}
}

func tokenize(command string) []string {
	var (
		tokens   []string
		current  strings.Builder
		inSingle bool
		inDouble bool
	)

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for i := 0; i < len(command); i++ {
		ch := command[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		}

		if inSingle || inDouble {
			current.WriteByte(ch)
			continue
		}

		switch ch {
		case ' ', '\t', '\n':
			flush()
		case '|', '<':
			flush()
			tokens = append(tokens, string(ch))
		case '>':
			flush()
			if i+1 < len(command) && command[i+1] == '>' {
				tokens = append(tokens, ">>")
				i++
			} else {
				tokens = append(tokens, ">")
			}
		default:
			current.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

func isPathLike(token string) bool {
	if token == "" || strings.HasPrefix(token, "-") {
		return false
	}
	return strings.Contains(token, "/") || strings.Contains(token, ".") || strings.HasPrefix(token, "~")
}

func uniqStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
