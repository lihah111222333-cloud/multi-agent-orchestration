package archive

import "testing"

func TestParseArchiveTimestamp(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "plain", raw: "1700000000001", want: 1700000000001},
		{name: "with suffix", raw: "1700000000001-2", want: 1700000000001},
		{name: "invalid", raw: "abc", want: 0},
		{name: "empty", raw: "", want: 0},
	}
	for _, tc := range tests {
		if got := ParseArchiveTimestamp(tc.raw); got != tc.want {
			t.Fatalf("%s: ParseArchiveTimestamp(%q)=%d, want %d", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestMergeThreadArchiveMaps(t *testing.T) {
	base := map[string]int64{
		"thread-a": 100,
	}
	disk := map[string]int64{
		"thread-a": 120,
		"thread-b": 80,
		"":         999,
	}
	got := MergeThreadArchiveMaps(base, disk)
	if got["thread-a"] != 120 {
		t.Fatalf("thread-a merged=%d, want %d", got["thread-a"], int64(120))
	}
	if got["thread-b"] != 80 {
		t.Fatalf("thread-b merged=%d, want %d", got["thread-b"], int64(80))
	}
	if _, ok := got[""]; ok {
		t.Fatalf("unexpected empty-thread entry in merged map: %v", got)
	}
}
