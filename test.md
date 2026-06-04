# Terminal Markdown Viewer

A **fast**, _beautiful_ terminal viewer for Markdown files.

## Features

- Pretty-rendered markdown view
- Inline editing with raw markdown
- Save and return to rendered view
- Syntax highlighting for code blocks

## Usage

```bash
tvmd path/to/file.md
```

## Keybindings

| Key | Action |
|-----|--------|
| `e` | Enter edit mode |
| `ctrl+s` | Save and return to view |
| `esc` | Cancel edit (discard changes) |
| `q` | Quit |
| `↑` / `↓` | Scroll |
| `g` / `G` | Jump to top / bottom |

## Example Code

```go
package main

import "fmt"

func main() {
    fmt.Println("Hello, world!")
}
```

> This is a blockquote with **bold** and _italic_ text.

---

Made with [charmbracelet](https://charm.sh) libraries.
