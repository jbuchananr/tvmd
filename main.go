package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type viewMode int

const (
	modeView viewMode = iota
	modeEditing
)

type editorFinishedMsg struct{ err error }

// VSCode-light chrome colors
var (
	tabBarBg = lipgloss.NewStyle().
			Background(lipgloss.Color("254")).
			Foreground(lipgloss.Color("240"))

	activeTab = lipgloss.NewStyle().
			Background(lipgloss.Color("255")).
			Foreground(lipgloss.Color("0")).
			Bold(true).
			Padding(0, 1)

	// Dark foreground is explicit so stripped-ANSI content (tables, code) is
	// readable even when the terminal's default foreground is light.
	whiteBg = lipgloss.NewStyle().Background(lipgloss.Color("255")).Foreground(lipgloss.Color("16"))
	grayBg  = lipgloss.NewStyle().Background(lipgloss.Color("253")).Foreground(lipgloss.Color("16"))

	statusBg = lipgloss.NewStyle().
			Background(lipgloss.Color("26")).
			Foreground(lipgloss.Color("255"))

	statusSaved = lipgloss.NewStyle().
			Background(lipgloss.Color("26")).
			Foreground(lipgloss.Color("119")).
			Bold(true)

	statusErr = lipgloss.NewStyle().
			Background(lipgloss.Color("160")).
			Foreground(lipgloss.Color("255"))
)

const mdStyle = `{
  "document":    { "background_color":"255","block_suffix":"\n","margin":2,"color":"16" },

  "block_quote": { "background_color":"255","indent":1,"indent_token":"│ ","color":"243","italic":true },

  "paragraph":   { "background_color":"255","block_suffix":"\n","color":"16" },
  "list":        { "background_color":"255","color":"16","level_indent":2 },
  "item":        { "background_color":"255","color":"16" },
  "enumeration": { "background_color":"255","color":"16","format":". " },

  "h1": { "background_color":"255","color":"16","bold":true,"block_prefix":"\n","block_suffix":"\n\n" },
  "h2": { "background_color":"255","color":"16","bold":true,"block_prefix":"\n","block_suffix":"\n" },
  "h3": { "background_color":"255","color":"16","bold":true,"italic":true,"block_suffix":"\n" },
  "h4": { "background_color":"255","color":"16","bold":true,"block_suffix":"\n" },
  "h5": { "background_color":"255","color":"16","block_suffix":"\n" },
  "h6": { "background_color":"255","color":"16","italic":true,"block_suffix":"\n" },

  "strong": { "background_color":"255","bold":true,"color":"16" },
  "emph":   { "background_color":"255","italic":true,"color":"16" },

  "hr":   { "color":"249" },
  "code": { "prefix":" ","suffix":" ","color":"124","background_color":"250" },

  "code_block": {
    "margin":2,
    "background_color":"253",
    "chroma_style":"github",
    "color":"16"
  },

  "table": {
    "center_separator":"+",
    "column_separator":"|",
    "row_separator":"-"
  },

  "link":       { "background_color":"255","color":"26","underline":true },
  "link_text":  { "background_color":"255","color":"26","bold":true },
  "image":      { "background_color":"255","color":"129","underline":true },
  "image_text": { "background_color":"255","color":"243","format":"Image: %s" }
}`

// ansiRe matches ANSI escape sequences so we can strip them for processing.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// hasCodeBg detects whether a raw glamour line carries any code-block gray (253) background.
func hasCodeBg(line string) bool { return strings.Contains(line, "48;5;253m") }

// unifyCodeBg replaces every white-bg (255) ANSI sequence in a code-block line
// with gray (253) so the entire line — text AND surrounding spaces — renders on
// the same gray background instead of appearing as isolated gray islands.
func unifyCodeBg(line string) string {
	return strings.ReplaceAll(line, "48;5;255m", "48;5;253m")
}


// leadingAnsiRe matches one or more ANSI escape sequences at the start of a string.
var leadingAnsiRe = regexp.MustCompile(`^(\x1b\[[0-9;]*m)+`)

// stripLeadingANSI removes all ANSI escape sequences at the very start of a
// line so that the bg wrapper applied by processContent reaches the first
// visible character without any intervening reset punching a black hole.
func stripLeadingANSI(s string) string {
	return leadingAnsiRe.ReplaceAllString(s, "")
}

// isTableSeparator returns true for lines that are pure dash/plus/space — the
// horizontal rule rows glamour emits between table header and body.
func isTableSeparator(clean string) bool {
	t := strings.TrimSpace(clean)
	if len(t) == 0 {
		return false
	}
	for _, c := range t {
		if c != '-' && c != '+' && c != ' ' {
			return false
		}
	}
	return true
}

type model struct {
	filepath string
	content  string
	mode     viewMode
	// viewport is used only for scroll state (YOffset, key handling).
	// We never call viewport.View() — we render processedLines directly so we
	// own every pixel and never hit the viewport's plain-space padding that
	// shows as black on the right/bottom edges.
	viewport       viewport.Model
	processedLines []string // display-ready lines, each exactly m.width wide
	width          int
	height         int
	err            error
	saved          bool
	ready          bool
}

func newModel(filepath, content string) model {
	return model{filepath: filepath, content: content, mode: modeView}
}

func (m model) Init() tea.Cmd { return nil }

func resolveEditor() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return "nano"
}

func openEditorCmd(filepath string) tea.Cmd {
	cmd := exec.Command(resolveEditor(), filepath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{err}
	})
}

func (m *model) renderMarkdown() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var opts []glamour.TermRendererOption
	if style := os.Getenv("GLAMOUR_STYLE"); style != "" {
		opts = append(opts, glamour.WithStandardStyle(style))
	} else {
		opts = append(opts, glamour.WithStylesFromJSONBytes([]byte(mdStyle)))
	}
	opts = append(opts, glamour.WithWordWrap(w))
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return m.content
	}
	out, err := r.Render(m.content)
	if err != nil {
		return m.content
	}
	return out
}


// buildLines renders markdown → glamour → per-line processed strings, each
// padded to exactly m.width with the correct background so the viewport's own
// inner padding never fires (it would use terminal-default bg = black).
func (m *model) buildLines(rendered string) {
	rawLines := strings.Split(rendered, "\n")
	m.processedLines = processLines(rawLines, m.width)
}

// processLines is the per-line version of processContent, taking a slice
// of raw glamour lines and returning display-ready strings.
func processLines(rawLines []string, width int) []string {
	out := make([]string, len(rawLines))
	for i, line := range rawLines {
		stripped := stripLeadingANSI(line)
		clean := stripANSI(line)
		switch {
		case isTableSeparator(clean):
			out[i] = whiteBg.Width(width).Render(clean)
		case strings.Contains(clean, "|"):
			out[i] = whiteBg.Width(width).Render(clean)
		case hasCodeBg(line):
			out[i] = grayBg.Width(width).Render(unifyCodeBg(stripped))
		case strings.TrimSpace(clean) == "":
			adjCode := (i > 0 && hasCodeBg(rawLines[i-1])) ||
				(i+1 < len(rawLines) && hasCodeBg(rawLines[i+1]))
			if adjCode {
				out[i] = grayBg.Width(width).Render("")
			} else {
				out[i] = whiteBg.Width(width).Render("")
			}
		default:
			out[i] = whiteBg.Width(width).Render(stripped)
		}
	}
	return out
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		viewportH := m.height - 2
		rendered := m.renderMarkdown()
		m.buildLines(rendered)
		if !m.ready {
			m.viewport = viewport.New(m.width, viewportH)
			// Give the viewport the raw content so it knows total line count
			// for scroll-limit clamping. We never call viewport.View().
			m.viewport.SetContent(rendered)
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = viewportH
			m.viewport.SetContent(rendered)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.mode == modeView {
				return m, tea.Quit
			}
		case "e":
			if m.mode == modeView {
				m.saved = false
				m.mode = modeEditing
				return m, openEditorCmd(m.filepath)
			}
		}

	case editorFinishedMsg:
		m.mode = modeView
		if msg.err != nil {
			m.err = msg.err
		} else {
			data, err := os.ReadFile(m.filepath)
			if err != nil {
				m.err = err
			} else {
				m.content = string(data)
				m.err = nil
				m.saved = true
				rendered := m.renderMarkdown()
				m.buildLines(rendered)
				m.viewport.SetContent(rendered)
				m.viewport.GotoTop()
			}
		}
		return m, nil
	}

	if m.mode == modeView {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Loading..."
	}
	var sb strings.Builder

	// ── Tab bar ──────────────────────────────────────────────────────────────
	tab := activeTab.Render(m.filepath)
	fill := tabBarBg.Width(max(0, m.width-lipgloss.Width(tab))).Render("")
	sb.WriteString(tab + fill + "\n")

	// ── Content — render directly from processedLines, bypassing viewport.View()
	// so we never hit the viewport's internal plain-space padding (black bg).
	viewH := m.height - 2
	yOff := m.viewport.YOffset
	for row := 0; row < viewH; row++ {
		idx := yOff + row
		if idx < len(m.processedLines) {
			sb.WriteString(m.processedLines[idx])
		} else {
			sb.WriteString(whiteBg.Width(m.width).Render(""))
		}
		sb.WriteByte('\n')
	}

	// ── Status bar ────────────────────────────────────────────────────────────
	if m.err != nil {
		sb.WriteString(statusErr.Width(m.width).Render(fmt.Sprintf("  ⚠  %s  ", m.err.Error())))
		return sb.String()
	}
	var left string
	if m.saved {
		left = statusSaved.Render(" ✓ saved ") + statusBg.Render(" ")
	} else {
		left = statusBg.Render("  ")
	}
	right := statusBg.Render(fmt.Sprintf("  e: edit (%s)   q: quit   ↑↓ scroll   g/G top/bottom  ", resolveEditor()))
	gap := statusBg.Width(max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))).Render("")
	sb.WriteString(left + gap + right)

	return sb.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: tvmd <file.md>")
		os.Exit(1)
	}
	fp := os.Args[1]
	data, err := os.ReadFile(fp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", fp, err)
		os.Exit(1)
	}
	p := tea.NewProgram(newModel(fp, string(data)), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
