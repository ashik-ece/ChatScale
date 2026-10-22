// Package filter implements the trie-based banned-word filter.
package filter

import (
	"strings"
	"unicode"
)

// TrieNode is one rune in the banned-word trie.
type TrieNode struct {
	children map[rune]*TrieNode
	isEnd    bool
}

// Trie stores banned words case-insensitively. It is built once at startup and
// read-only afterwards, so lookups need no locking.
type Trie struct {
	root *TrieNode
}

func NewTrie() *Trie {
	return &Trie{root: &TrieNode{children: make(map[rune]*TrieNode)}}
}

func (t *Trie) Insert(word string) {
	word = strings.ToLower(word)
	if word == "" {
		return
	}
	node := t.root
	for _, r := range word {
		next, ok := node.children[r]
		if !ok {
			next = &TrieNode{children: make(map[rune]*TrieNode)}
			node.children[r] = next
		}
		node = next
	}
	node.isEnd = true
}

// Contains reports whether word is exactly one of the inserted words.
func (t *Trie) Contains(word string) bool {
	node := t.root
	for _, r := range strings.ToLower(word) {
		next, ok := node.children[r]
		if !ok {
			return false
		}
		node = next
	}
	return node.isEnd && node != t.root
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
