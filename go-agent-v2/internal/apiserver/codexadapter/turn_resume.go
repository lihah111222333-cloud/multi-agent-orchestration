package codexadapter

import (
	lifecycleconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/lifecycle"
)

// FuzzyFileSearch walks directories and returns fuzzy-matched file paths.
func (a *Adapter) FuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	return lifecycleconsumer.FuzzyFileSearch(query, roots, fuzzyMatch)
}

func appendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	return lifecycleconsumer.AppendUniqueThreadIDFallback(dst, seen, candidate)
}

func PreviewResumeCandidates(candidates []string, limit int) []string {
	return lifecycleconsumer.PreviewResumeCandidates(candidates, limit)
}

func BuildResumeCandidates(threadID string, resolved []string, normalize func(string) string) []string {
	return lifecycleconsumer.BuildResumeCandidates(threadID, resolved, normalize)
}

func TryResumeCandidates(
	candidates []string,
	fallbackID string,
	resumeFn func(string) error,
	isCandidateError func(error) bool,
) (string, error) {
	return lifecycleconsumer.TryResumeCandidates(candidates, fallbackID, resumeFn, isCandidateError)
}

func IsHistoricalResumeCandidateError(err error) bool {
	return lifecycleconsumer.IsHistoricalResumeCandidateError(err)
}

func IsCodexProcessCrashError(err error) bool {
	return lifecycleconsumer.IsCodexProcessCrashError(err)
}
