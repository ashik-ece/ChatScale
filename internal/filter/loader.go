package filter

import (
	"bufio"
	"os"
	"strings"
)

// LoadBannedWords builds a Trie from a file with one word per line. Blank
// lines and lines starting with # are ignored.
func LoadBannedWords(path string) (*Trie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t := NewTrie()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		t.Insert(line)
	}
	return t, sc.Err()
}
