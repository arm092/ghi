package compiler

import (
	"go/token"
	"sort"
)

type sourceEdit struct {
	start, end int
	text       string
}

// Retain exact byte origins through width-changing surface syntax. Inserted
// lowering syntax has no origin; equal-looking user statements remain distinct.
type coverageSourceMap struct {
	original      []byte
	offsets       []int
	lines         []int
	originalLines []int
}

func (m *coverageSourceMap) apply(source []byte, edits []sourceEdit) {
	if m == nil || len(edits) == 0 {
		return
	}
	if m.offsets == nil {
		m.original = append([]byte(nil), source...)
		m.offsets = make([]int, len(source))
		for i := range m.offsets {
			m.offsets[i] = i
		}
	}
	next := make([]int, 0, len(source))
	previous := 0
	for _, edit := range edits {
		next = append(next, m.offsets[previous:edit.start]...)
		for range []byte(edit.text) {
			next = append(next, -1)
		}
		previous = edit.end
	}
	m.offsets = append(next, m.offsets[previous:]...)
}
func (m *coverageSourceMap) finish(source []byte) {
	if m.offsets == nil {
		return
	}
	m.lines = []int{0}
	m.originalLines = []int{0}
	for i, b := range m.original {
		if b == '\n' {
			m.originalLines = append(m.originalLines, i+1)
		}
	}
	for i, b := range source {
		if b == '\n' {
			m.lines = append(m.lines, i+1)
		}
	}
}
func (m *coverageSourceMap) position(pos token.Position) token.Position {
	if m == nil || m.offsets == nil {
		return pos
	}
	if pos.Line < 1 || pos.Line > len(m.lines) || pos.Column < 1 {
		return token.Position{}
	}
	offset := m.lines[pos.Line-1] + pos.Column - 1
	if offset < 0 || offset >= len(m.offsets) || m.offsets[offset] < 0 {
		return token.Position{}
	}
	offset = m.offsets[offset]
	lines := m.originalLines
	line := sort.Search(len(lines), func(i int) bool { return lines[i] > offset })
	pos.Line = line
	pos.Column = offset - lines[line-1] + 1
	return pos
}
