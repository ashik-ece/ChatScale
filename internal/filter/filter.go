package filter

import "strings"

const Mask = "****"

// Filter replaces every banned token in content with Mask and reports whether
// any was found. Tokens are runs of letters and digits, so whitespace and
// punctuation separate words and are preserved in the output.
func (t *Trie) Filter(content string) (filtered string, matched bool) {
	var out strings.Builder
	out.Grow(len(content))

	runes := []rune(content)
	for i := 0; i < len(runes); {
		if !isWordRune(runes[i]) {
			out.WriteRune(runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && isWordRune(runes[j]) {
			j++
		}
		token := string(runes[i:j])
		if t.Contains(token) {
			out.WriteString(Mask)
			matched = true
		} else {
			out.WriteString(token)
		}
		i = j
	}
	return out.String(), matched
}
