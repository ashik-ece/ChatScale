package filter

import "testing"

func newTestTrie() *Trie {
	t := NewTrie()
	for _, w := range []string{"badword", "spam", "NoOb"} {
		t.Insert(w)
	}
	return t
}

func TestContains(t *testing.T) {
	trie := newTestTrie()
	tests := []struct {
		word string
		want bool
	}{
		{"badword", true},
		{"BADWORD", true},
		{"BadWord", true},
		{"noob", true},
		{"hello", false},
		{"bad", false},      // prefix of a banned word is not banned
		{"badwords", false}, // longer than a banned word
		{"", false},
	}
	for _, tt := range tests {
		if got := trie.Contains(tt.word); got != tt.want {
			t.Errorf("Contains(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}

func TestFilter(t *testing.T) {
	trie := newTestTrie()
	tests := []struct {
		name        string
		in          string
		want        string
		wantMatched bool
	}{
		{"clean", "hello there", "hello there", false},
		{"single banned", "badword", "****", true},
		{"mixed case", "this is BaDwOrD ok", "this is **** ok", true},
		{"one banned among many", "you are a noob today", "you are a **** today", true},
		{"punctuation separates tokens", "spam, spam! (noob)", "****, ****! (****)", true},
		{"substring is not a match", "spammer badwords", "spammer badwords", false},
		{"whitespace preserved", "a  badword\tb", "a  ****\tb", true},
		{"unicode untouched", "héllo wörld badword", "héllo wörld ****", true},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matched := trie.Filter(tt.in)
			if got != tt.want || matched != tt.wantMatched {
				t.Errorf("Filter(%q) = (%q, %v), want (%q, %v)", tt.in, got, matched, tt.want, tt.wantMatched)
			}
		})
	}
}

func TestLoadBannedWords(t *testing.T) {
	trie, err := LoadBannedWords("../../configs/banned_words.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"badword", "BADWORD2", "spam", "griefer"} {
		if !trie.Contains(w) {
			t.Errorf("expected %q to be loaded", w)
		}
	}
	if trie.Contains("# One banned word per line; matching is case-insensitive.") {
		t.Error("comment line should not be loaded")
	}
}
