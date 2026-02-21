package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultCodeOpenContextLines = 90
	maxCodeOpenContextLines     = 180
	maxCodeOpenFileBytes        = 64 << 20 // 64MB
	binaryProbeBytes            = 8 << 10  // 8KB
)

type uiCodeOpenParams struct {
	FilePath string   `json:"filePath"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Context  int      `json:"context"`
	Project  string   `json:"project"`
	Projects []string `json:"projects"`
}

func normalizeCodeReferencePath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, `"'`)
	if parsed, err := url.Parse(value); err == nil && strings.EqualFold(parsed.Scheme, "file") {
		value = filepath.FromSlash(parsed.Path)
	}
	return strings.TrimSpace(value)
}

func normalizeProjectRoots(project string, projects []string) []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(projects)+2)
	appendRoot := func(raw string) {
		normalized := normalizeProjectPath(raw)
		if normalized == "" || normalized == "." {
			return
		}
		key := strings.ToLower(filepath.Clean(normalized))
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		roots = append(roots, normalized)
	}
	appendRoot(project)
	for _, item := range projects {
		appendRoot(item)
	}
	return roots
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func resolveCodeReferenceFilePath(rawPath, project string, projects []string) (string, error) {
	path := normalizeCodeReferencePath(rawPath)
	if path == "" {
		return "", apperrors.New("Server.uiCodeOpen", "filePath is required")
	}

	if filepath.IsAbs(path) && fileExists(path) {
		return path, nil
	}

	candidates := []string{path}
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		candidates = append(candidates, strings.TrimSpace(path[2:]))
	}

	roots := normalizeProjectRoots(project, projects)
	for _, relPath := range candidates {
		for _, root := range roots {
			joined := filepath.Join(root, relPath)
			if fileExists(joined) {
				return filepath.Clean(joined), nil
			}
		}
	}

	for _, relPath := range candidates {
		abs, err := filepath.Abs(relPath)
		if err != nil {
			continue
		}
		if fileExists(abs) {
			return abs, nil
		}
	}
	return "", apperrors.Newf("Server.uiCodeOpen", "file not found: %s", path)
}

func clampCodeContextLines(value int) int {
	if value <= 0 {
		return defaultCodeOpenContextLines
	}
	if value > maxCodeOpenContextLines {
		return maxCodeOpenContextLines
	}
	return value
}

func clampLine(value, total int) int {
	if total <= 0 {
		return 1
	}
	if value <= 0 {
		return 1
	}
	if value > total {
		return total
	}
	return value
}

func clampColumn(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func codePathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	normalized := filepath.ToSlash(abs)
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return (&url.URL{Scheme: "file", Path: normalized}).String()
}

func (s *Server) gatherCodeDiagnostics(filePath string, startLine, endLine int) []map[string]any {
	if s == nil {
		return []map[string]any{}
	}
	uri := codePathToURI(filePath)
	s.diagMu.RLock()
	diags := s.diagCache[uri]
	s.diagMu.RUnlock()
	if len(diags) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(diags))
	for _, diag := range diags {
		line := diag.Range.Start.Line + 1
		column := diag.Range.Start.Character + 1
		if line < startLine || line > endLine {
			continue
		}
		result = append(result, map[string]any{
			"line":     line,
			"column":   column,
			"severity": diag.Severity.String(),
			"message":  diag.Message,
		})
	}
	return result
}

func buildCodeSnippet(lines []string, startLine, endLine int) []map[string]any {
	if startLine <= 0 || endLine < startLine {
		return []map[string]any{}
	}
	snippet := make([]map[string]any, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		text := ""
		idx := line - 1
		if idx >= 0 && idx < len(lines) {
			text = lines[idx]
		}
		snippet = append(snippet, map[string]any{
			"line": line,
			"text": text,
		})
	}
	return snippet
}

func looksLikeBinaryContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	sample := content
	if len(sample) > binaryProbeBytes {
		sample = sample[:binaryProbeBytes]
	}
	nonTextBytes := 0
	for _, b := range sample {
		switch b {
		case 0:
			return true
		case '\n', '\r', '\t':
			continue
		}
		if b < 0x20 || b == 0x7f {
			nonTextBytes++
		}
	}
	// 宽松阈值，避免将 UTF-8 文本误判为二进制。
	return nonTextBytes*100 >= len(sample)*15
}

func detectMediaType(path string, content []byte) string {
	// 优先按扩展名判断（SVG 等格式被 http.DetectContentType 误判为 text/plain）。
	if byExt := mediaTypeByExtension(path); byExt != "" {
		return byExt
	}
	if len(content) > 0 {
		sniff := content
		if len(sniff) > 512 {
			sniff = sniff[:512]
		}
		detected := strings.TrimSpace(http.DetectContentType(sniff))
		if detected != "" && detected != "application/octet-stream" {
			return detected
		}
	}
	return "application/octet-stream"
}

func mediaTypeByExtension(path string) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}

func isImagePreviewExtension(path string) bool {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(filepath.Ext(path)), ".")) {
	case "png", "jpg", "jpeg", "svg":
		return true
	default:
		return false
	}
}

func resolveCodeOpenRelativePath(resolvedPath, project string, projects []string) string {
	relativePath := resolvedPath
	for _, root := range normalizeProjectRoots(project, projects) {
		rel, relErr := filepath.Rel(root, resolvedPath)
		if relErr != nil {
			continue
		}
		rel = filepath.Clean(rel)
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		relativePath = filepath.ToSlash(rel)
		break
	}
	return relativePath
}

func fileLanguageByPath(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return "text"
	}
	switch ext {
	case "go":
		return "go"
	case "rs":
		return "rust"
	case "ts", "tsx":
		return "typescript"
	case "js", "jsx":
		return "javascript"
	case "py":
		return "python"
	case "c", "h", "hpp", "cpp", "cc":
		return "c"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "md":
		return "markdown"
	case "css":
		return "css"
	case "html":
		return "html"
	case "java":
		return "java"
	case "kt":
		return "kotlin"
	case "swift":
		return "swift"
	default:
		return ext
	}
}

func (s *Server) uiCodeOpenTyped(_ context.Context, p uiCodeOpenParams) (any, error) {
	logger.Info("ui/code/open: begin",
		"file_path", strings.TrimSpace(p.FilePath),
		"line", p.Line,
		"column", p.Column,
		"project", strings.TrimSpace(p.Project),
		"projects_count", len(p.Projects),
	)

	resolvedPath, err := resolveCodeReferenceFilePath(p.FilePath, p.Project, p.Projects)
	if err != nil {
		logger.Warn("ui/code/open: resolve path failed",
			"file_path", strings.TrimSpace(p.FilePath),
			"project", strings.TrimSpace(p.Project),
			"projects_count", len(p.Projects),
			logger.FieldError, err,
		)
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		logger.Warn("ui/code/open: stat failed",
			"resolved_path", resolvedPath,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.uiCodeOpen", "stat file")
	}
	if info.IsDir() {
		logger.Warn("ui/code/open: path is directory",
			"resolved_path", resolvedPath,
		)
		return nil, apperrors.Newf("Server.uiCodeOpen", "path is directory: %s", resolvedPath)
	}
	lspSupported := supportsLSPFileType(resolvedPath)
	if info.Size() > maxCodeOpenFileBytes {
		logger.Warn("ui/code/open: file too large",
			"resolved_path", resolvedPath,
			"size_bytes", info.Size(),
			"max_bytes", maxCodeOpenFileBytes,
		)
		return nil, apperrors.Newf("Server.uiCodeOpen", "file too large: %d bytes", info.Size())
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		logger.Warn("ui/code/open: read failed",
			"resolved_path", resolvedPath,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.uiCodeOpen", "read file")
	}
	relativePath := resolveCodeOpenRelativePath(resolvedPath, p.Project, p.Projects)
	if isImagePreviewExtension(resolvedPath) {
		mediaType := detectMediaType(resolvedPath, content)
		targetLine := 1
		if p.Line > 0 {
			targetLine = p.Line
		}
		previewURL := codePathToURI(resolvedPath)
		logger.Info("ui/code/open: image parser applied",
			"resolved_path", resolvedPath,
			"relative_path", relativePath,
			"media_type", mediaType,
			"size_bytes", len(content),
		)
		return map[string]any{
			"ok":           true,
			"filePath":     resolvedPath,
			"relative":     relativePath,
			"line":         targetLine,
			"column":       clampColumn(p.Column),
			"startLine":    1,
			"endLine":      1,
			"totalLines":   1,
			"language":     fileLanguageByPath(resolvedPath),
			"context":      0,
			"snippet":      []map[string]any{{"line": 1, "text": fmt.Sprintf("[image preview: %s, %d bytes]", mediaType, len(content))}},
			"diagnostics":  []map[string]any{},
			"lspOpened":    false,
			"binary":       looksLikeBinaryContent(content),
			"mediaType":    mediaType,
			"sizeBytes":    len(content),
			"image":        true,
			"plugin":       "image-parser",
			"previewURL":   previewURL,
			"thumbnailURL": previewURL,
		}, nil
	}

	if looksLikeBinaryContent(content) {
		mediaType := detectMediaType(resolvedPath, content)
		targetLine := 1
		if p.Line > 0 {
			targetLine = p.Line
		}
		logger.Info("ui/code/open: binary content detected",
			"resolved_path", resolvedPath,
			"relative_path", relativePath,
			"media_type", mediaType,
			"size_bytes", len(content),
		)
		return map[string]any{
			"ok":         true,
			"filePath":   resolvedPath,
			"relative":   relativePath,
			"line":       targetLine,
			"column":     clampColumn(p.Column),
			"startLine":  1,
			"endLine":    1,
			"totalLines": 1,
			"language":   fileLanguageByPath(resolvedPath),
			"context":    0,
			"snippet": []map[string]any{
				{
					"line": 1,
					"text": fmt.Sprintf("[binary file omitted: %s, %d bytes]", mediaType, len(content)),
				},
			},
			"diagnostics": []map[string]any{},
			"lspOpened":   false,
			"binary":      true,
			"mediaType":   mediaType,
			"sizeBytes":   len(content),
		}, nil
	}

	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}

	targetLine := clampLine(p.Line, len(lines))
	targetColumn := clampColumn(p.Column)
	contextLines := clampCodeContextLines(p.Context)
	startLine := targetLine - contextLines
	if startLine < 1 {
		startLine = 1
	}
	endLine := targetLine + contextLines
	if endLine > len(lines) {
		endLine = len(lines)
	}

	lspOpened := false
	if s.lsp != nil && lspSupported {
		_ = s.lsp.OpenFile(resolvedPath, string(content))
		lspOpened = true
	}
	diagnostics := s.gatherCodeDiagnostics(resolvedPath, startLine, endLine)

	result := map[string]any{
		"ok":          true,
		"filePath":    resolvedPath,
		"relative":    relativePath,
		"line":        targetLine,
		"column":      targetColumn,
		"startLine":   startLine,
		"endLine":     endLine,
		"totalLines":  len(lines),
		"language":    fileLanguageByPath(resolvedPath),
		"context":     contextLines,
		"snippet":     buildCodeSnippet(lines, startLine, endLine),
		"diagnostics": diagnostics,
		"lspOpened":   lspOpened,
	}

	logger.Info("ui/code/open: success",
		"resolved_path", resolvedPath,
		"relative_path", relativePath,
		"line", targetLine,
		"column", targetColumn,
		"snippet_lines", endLine-startLine+1,
		"diagnostics_count", len(diagnostics),
		"lsp_opened", lspOpened,
	)

	return result, nil
}

func supportsLSPFileType(path string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return false
	}
	for _, item := range lsp.DefaultServers {
		for _, supportedExt := range item.Extensions {
			if supportedExt == ext {
				return true
			}
		}
	}
	return false
}
