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
	expressions   []surfaceExpressionOrigin
	original      []byte
	offsets       []int
	lines         []int
	originalLines []int
}

type surfaceExpressionOrigin struct {
	start, end                 int
	originalStart, originalEnd int
}

// Grammar-owned expression ranges retain full wrapper boundaries while
// inserted bytes inside the wrappers keep no individual source origin.
func (m *coverageSourceMap) applyExpression(source []byte, edits []sourceEdit, start, end int) {
	if m == nil {
		return
	}
	originalStart, originalLast := m.expressionBoundary(start), m.expressionBoundary(end-1)
	m.apply(source, edits)
	if originalStart >= 0 && originalLast >= originalStart {
		m.expressions = append(m.expressions, surfaceExpressionOrigin{
			start: editedSourceOffset(start, edits, false), end: editedSourceOffset(end, edits, true),
			originalStart: originalStart, originalEnd: originalLast + 1,
		})
	}
}

func (m *coverageSourceMap) expressionBoundary(offset int) int {
	if m.offsets == nil {
		return offset
	}
	if offset >= 0 && offset < len(m.offsets) && m.offsets[offset] >= 0 {
		return m.offsets[offset]
	}
	for _, expr := range m.expressions {
		if offset == expr.start {
			return expr.originalStart
		}
		if offset == expr.end-1 {
			return expr.originalEnd - 1
		}
	}
	return -1
}

func editedSourceOffset(offset int, edits []sourceEdit, afterInsertion bool) int {
	delta := 0
	for _, edit := range edits {
		if offset < edit.start {
			break
		}
		if edit.start == edit.end && offset == edit.start {
			if afterInsertion {
				delta += len(edit.text)
			}
			continue
		}
		if offset == edit.start {
			return offset + delta
		}
		if offset < edit.end {
			return -1
		}
		delta += len(edit.text) - (edit.end - edit.start)
	}
	return offset + delta
}

func (m *coverageSourceMap) apply(source []byte, edits []sourceEdit) {
	if m == nil || len(edits) == 0 {
		return
	}
	for i := range m.expressions {
		m.expressions[i].start = editedSourceOffset(m.expressions[i].start, edits, true)
		m.expressions[i].end = editedSourceOffset(m.expressions[i].end, edits, false)
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
