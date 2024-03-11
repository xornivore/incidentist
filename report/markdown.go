package report

import (
	"fmt"
	"strings"
)

type markdown struct {
	strings.Builder
}

func link(desc, link string) string {
	desc = strings.ReplaceAll(desc, "[", "|")
	desc = strings.ReplaceAll(desc, "]", "|")
	return fmt.Sprintf("[%s](%s)", desc, link)
}

func (m *markdown) heading(level int, header string) {
	m.WriteString(strings.Repeat("#", level) + " " + header)
	m.WriteString("\n\n")
}

func (m *markdown) br() {
	m.WriteString("\n")
}

func (m *markdown) para(p string) {
	m.WriteString(p + "\n\n")
}

func (m *markdown) unordered(level int, li string) {
	m.WriteString(strings.Repeat("  ", level-1) + "- " + li + "\n")
}
