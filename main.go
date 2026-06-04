package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ── Modes ─────────────────────────────────────────────────────────────────────

type viewMode int

const (
	modeView viewMode = iota
	modeEdit
)

type editorMode int

const (
	emNormal  editorMode = iota
	emInsert
	emCommand
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	tabBarBg = lipgloss.NewStyle().Background(lipgloss.Color("254")).Foreground(lipgloss.Color("240"))
	activeTab = lipgloss.NewStyle().Background(lipgloss.Color("255")).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)

	whiteBg = lipgloss.NewStyle().Background(lipgloss.Color("255")).Foreground(lipgloss.Color("16"))
	grayBg  = lipgloss.NewStyle().Background(lipgloss.Color("253")).Foreground(lipgloss.Color("16"))

	statusBg    = lipgloss.NewStyle().Background(lipgloss.Color("26")).Foreground(lipgloss.Color("255"))
	statusSaved = lipgloss.NewStyle().Background(lipgloss.Color("26")).Foreground(lipgloss.Color("119")).Bold(true)
	statusErr   = lipgloss.NewStyle().Background(lipgloss.Color("160")).Foreground(lipgloss.Color("255"))

	// Vim mode indicator bars
	normalMode  = lipgloss.NewStyle().Background(lipgloss.Color("28")).Foreground(lipgloss.Color("255")).Bold(true).Padding(0, 1)
	insertMode  = lipgloss.NewStyle().Background(lipgloss.Color("26")).Foreground(lipgloss.Color("255")).Bold(true).Padding(0, 1)
	commandMode = lipgloss.NewStyle().Background(lipgloss.Color("52")).Foreground(lipgloss.Color("255")).Bold(true).Padding(0, 1)
)

// ── Glamour style ─────────────────────────────────────────────────────────────
//
// Heading hierarchy:
//   H1 — UPPERCASE, bold, black  (most prominent)
//   H2 — bold, VSCode blue        (section level)
//   H3 — bold+italic, teal        (subsection)
//   H4–H6 — progressively lighter

const mdStyle = `{
  "document":    { "background_color":"255","block_suffix":"\n","margin":2,"color":"16" },

  "block_quote": { "background_color":"255","indent":1,"indent_token":"│ ","color":"243","italic":true },

  "paragraph":   { "background_color":"255","block_suffix":"\n","color":"16" },
  "list":        { "background_color":"255","color":"16","level_indent":2 },
  "item":        { "background_color":"255","color":"16" },
  "enumeration": { "background_color":"255","color":"16","format":". " },

  "h1": { "background_color":"255","color":"16","bold":true,"upper":true,
          "block_prefix":"\n","block_suffix":"\n\n" },
  "h2": { "background_color":"255","color":"20","bold":true,
          "block_prefix":"\n","block_suffix":"\n" },
  "h3": { "background_color":"255","color":"30","bold":true,"italic":true,
          "block_suffix":"\n" },
  "h4": { "background_color":"255","color":"37","bold":true,"block_suffix":"\n" },
  "h5": { "background_color":"255","color":"243","block_suffix":"\n" },
  "h6": { "background_color":"255","color":"243","italic":true,"block_suffix":"\n" },

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

// ── ANSI helpers ──────────────────────────────────────────────────────────────

var (
	ansiRe        = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	leadingAnsiRe = regexp.MustCompile(`^(\x1b\[[0-9;]*m)+`)
)

func stripANSI(s string) string        { return ansiRe.ReplaceAllString(s, "") }
func stripLeadingANSI(s string) string { return leadingAnsiRe.ReplaceAllString(s, "") }
func hasCodeBg(line string) bool       { return strings.Contains(line, "48;5;253m") }
func unifyCodeBg(line string) string   { return strings.ReplaceAll(line, "48;5;255m", "48;5;253m") }

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

// processLines converts raw glamour output into display-ready full-width lines.
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
			adj := (i > 0 && hasCodeBg(rawLines[i-1])) || (i+1 < len(rawLines) && hasCodeBg(rawLines[i+1]))
			if adj {
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

// ── Model ─────────────────────────────────────────────────────────────────────

type model struct {
	filepath       string
	content        string
	mode           viewMode
	viewport       viewport.Model
	processedLines []string

	// vim editor state
	ta       textarea.Model
	edMode   editorMode
	pendKey  string // tracks double-key sequences: dd, yy, gg
	yank     string
	cmdBuf   string // buffer for :commands

	width  int
	height int
	err    error
	saved  bool
	ready  bool
}

func newModel(filepath, content string) model {
	return model{filepath: filepath, content: content, mode: modeView}
}

func (m model) Init() tea.Cmd { return nil }

// ── Markdown rendering ────────────────────────────────────────────────────────

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

func (m *model) buildLines(rendered string) {
	m.processedLines = processLines(strings.Split(rendered, "\n"), m.width)
}

// ── Vim editor helpers ────────────────────────────────────────────────────────

func (m *model) initEditor() {
	ta := textarea.New()
	ta.SetValue(m.content)
	ta.CharLimit = 0
	ta.ShowLineNumbers = true
	ta.SetWidth(m.width)
	ta.SetHeight(m.height - 2)
	ta.Focus()
	m.ta = ta
	m.edMode = emNormal
	m.pendKey = ""
	m.cmdBuf = ""
}

// sendKey passes a synthetic key to the textarea (used in normal mode for navigation).
func (m *model) sendKey(k tea.KeyType) {
	m.ta, _ = m.ta.Update(tea.KeyMsg{Type: k})
}

func (m *model) deleteLine() {
	lines := strings.Split(m.ta.Value(), "\n")
	row := m.ta.Line()
	if row >= len(lines) {
		return
	}
	m.yank = lines[row]
	if len(lines) == 1 {
		m.ta.SetValue("")
		return
	}
	newLines := append(lines[:row:row], lines[row+1:]...)
	m.ta.SetValue(strings.Join(newLines, "\n"))
	// navigate back to the same row (or last if we deleted the last line)
	target := row
	if target >= len(newLines) {
		target = len(newLines) - 1
	}
	for i := 0; i < target; i++ {
		m.sendKey(tea.KeyDown)
	}
}

func (m *model) yankLine() {
	lines := strings.Split(m.ta.Value(), "\n")
	row := m.ta.Line()
	if row < len(lines) {
		m.yank = lines[row]
	}
}

func (m *model) pasteLine() {
	if m.yank == "" {
		return
	}
	lines := strings.Split(m.ta.Value(), "\n")
	row := m.ta.Line()
	insertAt := row + 1
	if insertAt > len(lines) {
		insertAt = len(lines)
	}
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, m.yank)
	newLines = append(newLines, lines[insertAt:]...)
	m.ta.SetValue(strings.Join(newLines, "\n"))
	for i := 0; i < insertAt; i++ {
		m.sendKey(tea.KeyDown)
	}
	m.sendKey(tea.KeyCtrlA) // start of line
}

func (m *model) saveAndReturn() {
	m.content = m.ta.Value()
	if err := os.WriteFile(m.filepath, []byte(m.content), 0644); err != nil {
		m.err = err
	} else {
		m.err = nil
		m.saved = true
	}
	m.mode = modeView
	rendered := m.renderMarkdown()
	m.buildLines(rendered)
	m.viewport.SetContent(rendered)
	m.viewport.GotoTop()
}

// ── Update ────────────────────────────────────────────────────────────────────

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
			m.viewport.SetContent(rendered)
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = viewportH
			m.viewport.SetContent(rendered)
		}
		if m.mode == modeEdit {
			m.ta.SetWidth(m.width)
			m.ta.SetHeight(m.height - 2)
		}

	case tea.KeyMsg:
		switch m.mode {

		// ── View mode keys ──────────────────────────────────────────────────
		case modeView:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "e":
				m.saved = false
				m.initEditor()
				m.mode = modeEdit
				return m, textarea.Blink
			}

		// ── Edit mode keys ──────────────────────────────────────────────────
		case modeEdit:
			switch m.edMode {

			case emNormal:
				key := msg.String()
				// Handle double-key sequences first
				if m.pendKey == "d" && key == "d" {
					m.deleteLine()
					m.pendKey = ""
					return m, nil
				}
				if m.pendKey == "y" && key == "y" {
					m.yankLine()
					m.pendKey = ""
					return m, nil
				}
				if m.pendKey == "g" && key == "g" {
					m.sendKey(tea.KeyCtrlHome)
					m.pendKey = ""
					return m, nil
				}
				m.pendKey = "" // clear stale pending

				switch key {
				// Enter insert modes
				case "i":
					m.edMode = emInsert
					return m, textarea.Blink
				case "a": // insert after cursor
					m.sendKey(tea.KeyRight)
					m.edMode = emInsert
					return m, textarea.Blink
				case "A": // insert at end of line
					m.sendKey(tea.KeyCtrlE)
					m.edMode = emInsert
					return m, textarea.Blink
				case "o": // new line below
					m.sendKey(tea.KeyCtrlE)
					m.sendKey(tea.KeyEnter)
					m.edMode = emInsert
					return m, textarea.Blink
				case "O": // new line above
					m.sendKey(tea.KeyCtrlA)
					m.sendKey(tea.KeyEnter)
					m.sendKey(tea.KeyUp)
					m.edMode = emInsert
					return m, textarea.Blink

				// Navigation
				case "h", "left":
					m.sendKey(tea.KeyLeft)
				case "l", "right":
					m.sendKey(tea.KeyRight)
				case "j", "down":
					m.sendKey(tea.KeyDown)
				case "k", "up":
					m.sendKey(tea.KeyUp)
				case "0":
					m.sendKey(tea.KeyCtrlA)
				case "$":
					m.sendKey(tea.KeyCtrlE)
				case "G":
					m.sendKey(tea.KeyCtrlEnd)
				case "g":
					m.pendKey = "g"

				// Edit operations
				case "x": // delete char under cursor
					m.sendKey(tea.KeyDelete)
				case "d":
					m.pendKey = "d"
				case "y":
					m.pendKey = "y"
				case "p":
					m.pasteLine()

				// Save shortcut
				case "ctrl+s":
					m.saveAndReturn()
					return m, nil

				// Enter command mode
				case ":":
					m.edMode = emCommand
					m.cmdBuf = ":"

				// Exit editor
				case "ctrl+c":
					m.mode = modeView
					return m, nil
				}

			case emInsert:
				switch msg.String() {
				case "esc":
					m.edMode = emNormal
					m.sendKey(tea.KeyLeft) // vim: cursor moves back on leaving insert
					return m, nil
				case "ctrl+s": // quick save
					m.saveAndReturn()
					return m, nil
				case "ctrl+c":
					m.mode = modeView
					return m, nil
				default:
					var cmd tea.Cmd
					m.ta, cmd = m.ta.Update(msg)
					cmds = append(cmds, cmd)
				}

			case emCommand:
				switch msg.String() {
				case "esc":
					m.edMode = emNormal
					m.cmdBuf = ""
					return m, nil
				case "enter":
					cmd := strings.TrimPrefix(m.cmdBuf, ":")
					switch cmd {
					case "w":
						m.saveAndReturn()
					case "q", "q!":
						m.mode = modeView
					case "wq", "x":
						m.saveAndReturn()
					}
					m.cmdBuf = ""
					m.edMode = emNormal
					return m, nil
				case "backspace":
					if len(m.cmdBuf) > 1 {
						m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
					}
				default:
					m.cmdBuf += msg.String()
				}
			}
		}
	}

	// Delegate scroll/viewport in view mode
	if m.mode == modeView {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "\n  Loading..."
	}
	var sb strings.Builder

	switch m.mode {

	// ── View mode ─────────────────────────────────────────────────────────
	case modeView:
		tab := activeTab.Render(m.filepath)
		fill := tabBarBg.Width(max(0, m.width-lipgloss.Width(tab))).Render("")
		sb.WriteString(tab + fill + "\n")

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

		if m.err != nil {
			sb.WriteString(statusErr.Width(m.width).Render(fmt.Sprintf("  ⚠  %s  ", m.err.Error())))
		} else {
			var left string
			if m.saved {
				left = statusSaved.Render(" ✓ saved ") + statusBg.Render(" ")
			} else {
				left = statusBg.Render("  ")
			}
			right := statusBg.Render("  e: edit   q: quit   ↑↓/jk scroll   g/G top/bottom  ")
			gap := statusBg.Width(max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))).Render("")
			sb.WriteString(left + gap + right)
		}

	// ── Edit mode ─────────────────────────────────────────────────────────
	case modeEdit:
		// Tab bar with EDIT indicator
		title := m.filepath + "  [EDIT]"
		tab := activeTab.Render(title)
		fill := tabBarBg.Width(max(0, m.width-lipgloss.Width(tab))).Render("")
		sb.WriteString(tab + fill + "\n")

		// Textarea
		sb.WriteString(m.ta.View())
		sb.WriteByte('\n')

		// Vim mode status bar
		var modeLabel lipgloss.Style
		var modeText string
		switch m.edMode {
		case emNormal:
			modeLabel = normalMode
			modeText = "NORMAL"
		case emInsert:
			modeLabel = insertMode
			modeText = "INSERT"
		case emCommand:
			modeLabel = commandMode
			modeText = "COMMAND"
		}
		left := modeLabel.Render(" " + modeText + " ")

		var right string
		if m.edMode == emCommand {
			right = statusBg.Render("  " + m.cmdBuf + "█  ")
		} else {
			right = statusBg.Render(fmt.Sprintf("  Ln %d   i:insert  :w save  :q quit  ctrl+s save  ", m.ta.Line()+1))
		}
		gap := statusBg.Width(max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))).Render("")
		sb.WriteString(left + gap + right)
	}

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
