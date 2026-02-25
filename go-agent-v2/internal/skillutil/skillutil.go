package skillutil

import (
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// NormalizeName validates skill name.
func NormalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", apperrors.New("normalizeSkillName", "skill name is required")
	}
	return name, nil
}

// CollectInputSkillNames collects lowercase skill names from typed user inputs.
func CollectInputSkillNames[T any](inputs []T, typeOf func(T) string, nameOf func(T) string) map[string]struct{} {
	if len(inputs) == 0 || typeOf == nil || nameOf == nil {
		return nil
	}
	set := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if !strings.EqualFold(strings.TrimSpace(typeOf(input)), "skill") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(nameOf(input)))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}
