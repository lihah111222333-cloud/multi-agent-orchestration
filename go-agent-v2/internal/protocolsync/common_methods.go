package protocolsync

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/util"
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

func FindProtocolCommonPath() (string, error) {
	const rel = "app-server-protocol/src/protocol/common.rs"

	if explicit := strings.TrimSpace(os.Getenv("CODEX_RS_PROTOCOL_COMMON")); explicit != "" {
		if util.FileExists(explicit) {
			return explicit, nil
		}
		return "", apperrors.Newf("protocolsync.FindProtocolCommonPath", "CODEX_RS_PROTOCOL_COMMON not found: %s", explicit)
	}

	if root := strings.TrimSpace(os.Getenv("CODEX_RS_ROOT")); root != "" {
		candidate := filepath.Join(root, rel)
		if util.FileExists(candidate) {
			return candidate, nil
		}
		return "", apperrors.Newf("protocolsync.FindProtocolCommonPath", "CODEX_RS_ROOT set but common.rs missing: %s", candidate)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", apperrors.New("protocolsync.FindProtocolCommonPath", "cannot resolve current file path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	for _, candidate := range [...]string{
		filepath.Clean(filepath.Join(moduleRoot, "..", "codex-rs", rel)),
		filepath.Clean(filepath.Join(moduleRoot, "..", "codex", "codex-rs", rel)),
		filepath.Clean(filepath.Join(moduleRoot, "..", "..", "codex", "codex-rs", rel)),
		filepath.Clean(filepath.Join(moduleRoot, "..", "..", "..", "codex", "codex-rs", rel)),
	} {
		if util.FileExists(candidate) {
			return candidate, nil
		}
	}

	return "", apperrors.New("protocolsync.FindProtocolCommonPath", "codex-rs protocol/common.rs not found; set CODEX_RS_PROTOCOL_COMMON or CODEX_RS_ROOT")
}

func LoadMethodCatalog(commonPath string) (MethodCatalog, error) {
	content, err := os.ReadFile(commonPath)
	if err != nil {
		return MethodCatalog{}, apperrors.Wrap(err, "protocolsync.LoadMethodCatalog", "read protocol common.rs")
	}

	requests, err := parseMacroMethods(string(content), "server_request_definitions!")
	if err != nil {
		return MethodCatalog{}, apperrors.Wrap(err, "protocolsync.LoadMethodCatalog", "parse server requests")
	}
	notifications, err := parseMacroMethods(string(content), "server_notification_definitions!")
	if err != nil {
		return MethodCatalog{}, apperrors.Wrap(err, "protocolsync.LoadMethodCatalog", "parse server notifications")
	}

	if len(requests) == 0 || len(notifications) == 0 {
		return MethodCatalog{}, apperrors.Newf("protocolsync.LoadMethodCatalog", "parsed empty methods (requests=%d notifications=%d)", len(requests), len(notifications))
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
		} else if match := serdeRenamePattern.FindStringSubmatch(line); len(match) == 2 {
			pendingRename = match[1]
		} else if match := arrowPattern.FindStringSubmatch(line); len(match) == 3 {
			methods[match[2]] = struct{}{}
			pendingRename = ""
		} else if match := variantPattern.FindStringSubmatch(line); len(match) == 3 {
			if pendingRename != "" {
				methods[pendingRename] = struct{}{}
				pendingRename = ""
			} else {
				methods[toLowerCamel(match[1])] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, apperrors.Wrap(err, "protocolsync.parseMacroMethods", "scan macro block")
	}

	return methods, nil
}

func extractMacroBlock(content, macroName string) (string, error) {
	macroPos := strings.Index(content, macroName)
	if macroPos < 0 {
		return "", apperrors.Newf("protocolsync.extractMacroBlock", "macro not found: %s", macroName)
	}

	openPos := strings.Index(content[macroPos:], "{")
	if openPos < 0 {
		return "", apperrors.Newf("protocolsync.extractMacroBlock", "macro body start not found: %s", macroName)
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
					return "", apperrors.Newf("protocolsync.extractMacroBlock", "invalid macro body bounds: %s", macroName)
				}
				return content[start:i], nil
			}
		}
	}

	return "", apperrors.Newf("protocolsync.extractMacroBlock", "macro body end not found: %s", macroName)
}

func toLowerCamel(value string) string {
	if value == "" {
		return value
	}

	if !strings.Contains(value, "_") {
		return strings.ToLower(value[:1]) + value[1:]
	}

	parts := strings.Split(value, "_")
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		part = strings.ToLower(part)
		if builder.Len() == 0 {
			builder.WriteString(part)
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	return builder.String()
}
