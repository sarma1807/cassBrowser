package main

import (
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// fyneWordSeparators mirrors the private wordSeparator constant in fyne.io/fyne/v2/widget/entry.go.
// Characters in this set are treated as whitespace by the word-move logic.
const fyneWordSeparators = "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?"

// SafeMultiLineEntry wraps widget.Entry to work around a Fyne v2.4.4 bug in
// getTextWhitespaceRegion: when the cursor is at column 0 on a line whose
// characters are all whitespace or word separators, word-move shortcuts
// (Ctrl+Left/Right) trigger a slice-bounds panic.
type SafeMultiLineEntry struct {
	widget.Entry
}

func newSafeMultiLineEntry() *SafeMultiLineEntry {
	e := &SafeMultiLineEntry{}
	e.ExtendBaseWidget(e)
	e.MultiLine = true
	return e
}

// TypedShortcut intercepts word-move shortcuts before they reach Fyne's entry
// handler, skipping them when the cursor is in the position that would panic.
func (s *SafeMultiLineEntry) TypedShortcut(shortcut fyne.Shortcut) {
	if cs, ok := shortcut.(*desktop.CustomShortcut); ok {
		if cs.KeyName == fyne.KeyLeft || cs.KeyName == fyne.KeyRight {
			if s.wouldCrashOnWordMove() {
				return
			}
		}
	}
	s.Entry.TypedShortcut(shortcut)
}

// wouldCrashOnWordMove returns true when the cursor is at column 0 and the
// current line has no word characters (all runes are whitespace or Fyne word
// separators). This is the exact condition that causes getTextWhitespaceRegion
// in Fyne v2.4.4 to compute a negative slice index.
func (s *SafeMultiLineEntry) wouldCrashOnWordMove() bool {
	if s.CursorColumn != 0 {
		return false
	}
	lines := strings.Split(s.Text, "\n")
	if s.CursorRow >= len(lines) {
		return false
	}
	line := []rune(lines[s.CursorRow])
	if len(line) == 0 {
		return false // empty line: Fyne returns {-1,-1} safely
	}
	if !unicode.IsSpace(line[0]) && !strings.ContainsRune(fyneWordSeparators, line[0]) {
		return false // first char is a word char, no crash possible
	}
	for _, r := range line {
		if !unicode.IsSpace(r) && !strings.ContainsRune(fyneWordSeparators, r) {
			return false
		}
	}
	return true
}
