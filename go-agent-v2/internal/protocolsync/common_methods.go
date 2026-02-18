package protocolsync

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

type MethodCatalog struct {
	ServerRequests      map[string]struct{}
	ServerNotifications map[string]struct{}
}

func (c MethodCatalog) All() map[string]struct{} {
	out := make(map[string]struct{}, len(c.ServerRequests)+len(c.ServerNotifications))
	for method := range c.ServerRequests {
		out[method] = struct{}{}
	}
	for method := range c.ServerNotifications {
		out[method] = struct{}{}
	}
	return out
}

func (c MethodCatalog) SortedServerRequests() []string {
	return sortedKeys(c.ServerRequests)
}

func (c MethodCatalog) SortedServerNotifications() []string {
	return sortedKeys(c.ServerNotifications)
}

func (c MethodCatalog) SortedAll() []string {
	return sortedKeys(c.All())
}

func LoadDefaultMethodCatalog() (MethodCatalog, string, error) {
	commonPath, err := FindProtocolCommonPath()
	if err != nil {
		return MethodCatalog{}, "", err
	}
	catalog, err := LoadMethodCatalog(commonPath)
	if err != nil {
		return MethodCatalog{}, "", err
	}
	return catalog, commonPath, nil
}

func FindProtocolCommonPath() (string, error) {
	const rel = "app-server-protocol/src/protocol/common.rs"

	if explicit := strings.TrimSpace(os.Getenv("CODEX_RS_PROTOCOL_COMMON")); explicit != "" {
		if fileExists(explicit) {
			return explicit, nil
		}
		return "", fmt.Errorf("CODEX_RS_PROTOCOL_COMMON not found: %s", explicit)
	}

	if root := strings.TrimSpace(os.Getenv("CODEX_RS_ROOT")); root != "" {
		candidate := filepath.Join(root, rel)
		if fileExists(candidate) {
			return candidate, nil
		}
		return "", fmt.Errorf("CODEX_RS_ROOT set but common.rs missing: %s", candidate)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot resolve current file path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	var candidates []string
	candidates = append(candidates,
		filepath.Join(moduleRoot, "..", "codex-rs", rel),
		filepath.Join(moduleRoot, "..", "codex", "codex-rs", rel),
		filepath.Join(moduleRoot, "..", "..", "codex", "codex-rs", rel),
		filepath.Join(moduleRoot, "..", "..", "..", "codex", "codex-rs", rel),
	)

	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if fileExists(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("codex-rs protocol/common.rs not found; set CODEX_RS_PROTOCOL_COMMON or CODEX_RS_ROOT")
}

func LoadMethodCatalog(commonPath string) (MethodCatalog, error) {
	contentBytes, err := os.ReadFile(commonPath)
	if err != nil {
		return MethodCatalog{}, fmt.Errorf("read protocol common.rs: %w", err)
	}
	content := string(contentBytes)

	requests, err := parseMacroMethods(content, "server_request_definitions!")
	if err != nil {
		return MethodCatalog{}, fmt.Errorf("parse server requests: %w", err)
	}
	notifications, err := parseMacroMethods(content, "server_notification_definitions!")
	if err != nil {
		return MethodCatalog{}, fmt.Errorf("parse server notifications: %w", err)
	}

	if len(requests) == 0 || len(notifications) == 0 {
		return MethodCatalog{}, fmt.Errorf("parsed empty methods (requests=%d notifications=%d)", len(requests), len(notifications))
	}

	return MethodCatalog{
		ServerRequests:      requests,
		ServerNotifications: notifications,
	}, nil
}

func parseMacroMethods(content, macroName string) (map[string]struct{}, error) {
	block, err := extractMacroBlock(content, macroName)
	if err != nil {
		return nil, err
	}

	arrowPattern := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=>\s*"([^"]+)"`)
	variantPattern := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(\(|\{)`)
	serdeRenamePattern := regexp.MustCompile(`^#\s*\[\s*serde\s*\(\s*rename\s*=\s*"([^"]+)"\s*\)\s*\]`)

	methods := make(map[string]struct{})
	pendingRename := ""

	scanner := bufio.NewScanner(strings.NewReader(block))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if match := serdeRenamePattern.FindStringSubmatch(line); len(match) == 2 {
			pendingRename = match[1]
			continue
		}

		if match := arrowPattern.FindStringSubmatch(line); len(match) == 3 {
			methods[match[2]] = struct{}{}
			pendingRename = ""
			continue
		}

		if match := variantPattern.FindStringSubmatch(line); len(match) == 3 {
			if pendingRename != "" {
				methods[pendingRename] = struct{}{}
				pendingRename = ""
				continue
			}
			methods[toLowerCamel(match[1])] = struct{}{}
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan macro block: %w", err)
	}

	return methods, nil
}

func extractMacroBlock(content, macroName string) (string, error) {
	macroPos := strings.Index(content, macroName)
	if macroPos < 0 {
		return "", fmt.Errorf("macro not found: %s", macroName)
	}

	openPos := strings.Index(content[macroPos:], "{")
	if openPos < 0 {
		return "", fmt.Errorf("macro body start not found: %s", macroName)
	}
	openPos += macroPos

	depth := 0
	start := -1
	for i := openPos; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
			if depth == 1 {
				start = i + 1
			}
		case '}':
			depth--
			if depth == 0 {
				if start < 0 || start > i {
					return "", fmt.Errorf("invalid macro body bounds: %s", macroName)
				}
				return content[start:i], nil
			}
		}
	}

	return "", fmt.Errorf("macro body end not found: %s", macroName)
}

func toLowerCamel(value string) string {
	if value == "" {
		return value
	}

	if strings.Contains(value, "_") {
		parts := strings.Split(value, "_")
		var builder strings.Builder
		for index, part := range parts {
			if part == "" {
				continue
			}
			if index == 0 {
				builder.WriteString(strings.ToLower(part[:1]))
				if len(part) > 1 {
					builder.WriteString(part[1:])
				}
				continue
			}
			builder.WriteString(strings.ToUpper(part[:1]))
			if len(part) > 1 {
				builder.WriteString(part[1:])
			}
		}
		return builder.String()
	}

	return strings.ToLower(value[:1]) + value[1:]
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
