package lsp

import (
	"path/filepath"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func normalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "ts", "js", "javascript", "typescript", "jsx", "tsx":
		return "typescript"
	case "rs", "rust":
		return "rust"
	case "golang", "go":
		return "go"
	case "py", "python":
		return "python"
	case "c", "cpp", "cc", "cxx", "c++":
		return "c"
	default:
		return strings.ToLower(strings.TrimSpace(language))
	}
}

func (m *Manager) languageForFile(filePath string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))), ".")
	if ext == "" {
		return ""
	}
	m.mu.RLock()
	cfg := m.configs[ext]
	m.mu.RUnlock()
	if cfg == nil {
		return ""
	}
	return normalizeLanguage(cfg.Language)
}

func (m *Manager) ensureClientForFile(filePath string) (*Client, *ServerConfig, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))), ".")
	if ext == "" {
		return nil, nil, apperrors.Newf("LSP.ensureClientForFile", "unsupported file path (missing extension): %q", filePath)
	}

	m.mu.RLock()
	cfg := m.configs[ext]
	m.mu.RUnlock()
	if cfg == nil {
		return nil, nil, apperrors.Newf("LSP.ensureClientForFile", "no language server configured for extension: .%s", ext)
	}

	client, err := m.ensureClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

func (m *Manager) ensureClientForLanguage(language string) (*Client, *ServerConfig, error) {
	normalized := normalizeLanguage(language)
	if normalized == "" {
		return nil, nil, apperrors.Newf("LSP.ensureClientForLanguage", "language is required")
	}

	m.mu.RLock()
	cfg := m.languages[normalized]
	m.mu.RUnlock()
	if cfg == nil {
		return nil, nil, apperrors.Newf("LSP.ensureClientForLanguage", "no language server configured for language: %s", normalized)
	}

	client, err := m.ensureClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

func (m *Manager) ensureLanguageClientStarted(language string) error {
	_, _, err := m.ensureClientForLanguage(language)
	return err
}

func (m *Manager) restartClientForLanguage(language string) (*Client, *ServerConfig, error) {
	normalized := normalizeLanguage(language)
	if normalized == "" {
		return nil, nil, apperrors.Newf("LSP.restartClientForLanguage", "language is required")
	}

	m.mu.Lock()
	cfg := m.languages[normalized]
	if cfg == nil {
		m.mu.Unlock()
		return nil, nil, apperrors.Newf("LSP.restartClientForLanguage", "no language server configured for language: %s", normalized)
	}
	if existing := m.clients[cfg.Language]; existing != nil {
		_ = existing.Stop()
		delete(m.clients, cfg.Language)
	}
	m.mu.Unlock()

	client, err := m.ensureClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

func (m *Manager) resolveWorkspaceSymbolLanguage(filePath, language string) (string, error) {
	path := strings.TrimSpace(filePath)
	argLang := normalizeLanguage(language)

	switch {
	case path == "" && argLang == "":
		return "", apperrors.Newf("LSP.resolveWorkspaceSymbolLanguage", "exactly one of file_path or language is required")
	case path != "" && argLang != "":
		return "", apperrors.Newf("LSP.resolveWorkspaceSymbolLanguage", "file_path and language are mutually exclusive")
	case path == "":
		if !m.hasLanguageConfig(argLang) {
			return "", apperrors.Newf("LSP.resolveWorkspaceSymbolLanguage", "no language server configured for language: %s", argLang)
		}
		return argLang, nil
	default:
		pathLang := m.languageForFile(path)
		if pathLang == "" {
			return "", apperrors.Newf("LSP.resolveWorkspaceSymbolLanguage", "cannot infer language from file_path: %s", filePath)
		}
		return pathLang, nil
	}
}

func (m *Manager) hasLanguageConfig(language string) bool {
	normalized := normalizeLanguage(language)
	if normalized == "" {
		return false
	}
	m.mu.RLock()
	_, ok := m.languages[normalized]
	m.mu.RUnlock()
	return ok
}

// BootstrapLanguage 按语言启动对应 client，不要求 file_path。
func (m *Manager) BootstrapLanguage(language string) error {
	return m.ensureLanguageClientStarted(language)
}

// BootstrapLanguageFromFile 按 file_path 推导语言并启动对应 client。
func (m *Manager) BootstrapLanguageFromFile(filePath string) error {
	_, _, err := m.ensureClientForFile(filePath)
	return err
}
