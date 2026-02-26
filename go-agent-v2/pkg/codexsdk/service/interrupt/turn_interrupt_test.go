package interrupt

import "testing"

func TestIsInterruptTimeoutError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "timeout", err: stubErr("turn/interrupt timeout"), want: true},
		{name: "deadline", err: stubErr("context deadline exceeded"), want: true},
		{name: "other", err: stubErr("method not found"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isInterruptTimeoutError(tc.err); got != tc.want {
				t.Fatalf("isInterruptTimeoutError()=%v, want %v (err=%v)", got, tc.want, tc.err)
			}
		})
	}
}

type stubErr string

func (e stubErr) Error() string { return string(e) }
