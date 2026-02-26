package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	lifecycle "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
)

// FuzzyFileSearch walks directories and returns fuzzy-matched file paths.
func (a *Adapter) FuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	return lifecycle.FuzzyFileSearch(query, roots, fuzzyMatch)
}

func appendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	return common.AppendUniqueThreadIDFallback(dst, seen, candidate)
}

func PreviewResumeCandidates(candidates []string, limit int) []string {
	return lifecycle.PreviewResumeCandidates(candidates, limit)
}

func BuildResumeCandidates(threadID string, resolved []string, normalize func(string) string) []string {
	return lifecycle.BuildResumeCandidates(threadID, resolved, normalize)
}

func TryResumeCandidates(
	candidates []string,
	fallbackID string,
	resumeFn func(string) error,
	isCandidateError func(error) bool,
) (string, error) {
	return lifecycle.TryResumeCandidates(candidates, fallbackID, resumeFn, isCandidateError)
}

func IsHistoricalResumeCandidateError(err error) bool {
	return lifecycle.IsHistoricalResumeCandidateError(err)
}

func IsCodexProcessCrashError(err error) bool {
	return lifecycle.IsCodexProcessCrashError(err)
}
