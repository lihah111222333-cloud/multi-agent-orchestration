package lsp

import (
	"regexp"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

var (
	markdownATXHeadingPattern      = regexp.MustCompile(`^\s{0,3}(#{1,6})\s+(.+?)\s*$`)
	markdownSetextUnderlinePattern = regexp.MustCompile(`^\s{0,3}(=+|-+)\s*$`)
	markdownTrailingFencePattern   = regexp.MustCompile(`\s+#+\s*$`)
)

type markdownHeading struct {
	Level      int
	Line       int
	LineText   string
	Title      string
	TitleStart int
}

type markdownSymbolNode struct {
	Level    int
	Symbol   DocumentSymbol
	Children []*markdownSymbolNode
}

func (m *Manager) markdownDocumentSymbols(filePath string) ([]DocumentSymbol, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, apperrors.Newf("LSP.DocumentSymbol", "file_path is required")
	}
	uri := pathToURI(filePath)
	lock := m.documentLock(uri)
	lock.Lock()
	defer lock.Unlock()

	state, err := m.bootstrapDocumentWithoutClientLocked(filePath, uri, "markdown")
	if err != nil {
		return nil, apperrors.Wrap(err, "LSP.DocumentSymbol", "bootstrap markdown document")
	}
	return markdownSymbolsFromContent(state.Content), nil
}

func markdownSymbolsFromContent(content string) []DocumentSymbol {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	headings := parseMarkdownHeadings(lines)
	if len(headings) == 0 {
		return nil
	}
	return buildMarkdownSymbolTree(headings)
}

func parseMarkdownHeadings(lines []string) []markdownHeading {
	headings := make([]markdownHeading, 0)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if match := markdownATXHeadingPattern.FindStringSubmatch(line); match != nil {
			title := cleanMarkdownHeadingTitle(match[2])
			if title != "" {
				headings = append(headings, markdownHeading{
					Level:      len(match[1]),
					Line:       i,
					LineText:   line,
					Title:      title,
					TitleStart: headingTitleStart(line, title),
				})
			}
			continue
		}

		if i+1 >= len(lines) {
			continue
		}
		underlineMatch := markdownSetextUnderlinePattern.FindStringSubmatch(lines[i+1])
		if underlineMatch == nil {
			continue
		}
		title := strings.TrimSpace(line)
		if title == "" {
			continue
		}
		level := 2
		if strings.HasPrefix(strings.TrimSpace(underlineMatch[1]), "=") {
			level = 1
		}
		headings = append(headings, markdownHeading{
			Level:      level,
			Line:       i,
			LineText:   line,
			Title:      title,
			TitleStart: headingTitleStart(line, title),
		})
		i++ // skip setext underline line
	}
	return headings
}

func buildMarkdownSymbolTree(headings []markdownHeading) []DocumentSymbol {
	roots := make([]*markdownSymbolNode, 0, len(headings))
	stack := make([]*markdownSymbolNode, 0, len(headings))
	for _, heading := range headings {
		lineLen := len(heading.LineText)
		if lineLen < 0 {
			lineLen = 0
		}
		selectionStart := heading.TitleStart
		if selectionStart < 0 {
			selectionStart = 0
		}
		selectionEnd := selectionStart + len(heading.Title)
		if selectionEnd > lineLen {
			selectionEnd = lineLen
		}

		node := &markdownSymbolNode{
			Level: heading.Level,
			Symbol: DocumentSymbol{
				Name: heading.Title,
				Kind: SKNamespace,
				Range: Range{
					Start: Position{Line: heading.Line, Character: 0},
					End:   Position{Line: heading.Line, Character: lineLen},
				},
				SelectionRange: Range{
					Start: Position{Line: heading.Line, Character: selectionStart},
					End:   Position{Line: heading.Line, Character: selectionEnd},
				},
			},
		}

		for len(stack) > 0 && stack[len(stack)-1].Level >= heading.Level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, node)
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}

	result := make([]DocumentSymbol, 0, len(roots))
	for _, root := range roots {
		result = append(result, root.toDocumentSymbol())
	}
	return result
}

func (n *markdownSymbolNode) toDocumentSymbol() DocumentSymbol {
	if n == nil {
		return DocumentSymbol{}
	}
	out := n.Symbol
	if len(n.Children) == 0 {
		return out
	}
	out.Children = make([]DocumentSymbol, 0, len(n.Children))
	for _, child := range n.Children {
		out.Children = append(out.Children, child.toDocumentSymbol())
	}
	return out
}

func cleanMarkdownHeadingTitle(raw string) string {
	title := strings.TrimSpace(raw)
	title = markdownTrailingFencePattern.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func headingTitleStart(line, title string) int {
	idx := strings.Index(line, title)
	if idx < 0 {
		return 0
	}
	return idx
}
