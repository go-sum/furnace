package verify

import (
	"testing"
)

func TestDigestHexBytes(t *testing.T) {
	cases := []struct {
		ref  string
		want string // hex expected to decode
		ok   bool
	}{
		{
			ref:  "ghcr.io/org/repo:v1.0.0@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			want: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			ok:   true,
		},
		{
			ref:  "sha256:deadbeef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef",
			want: "deadbeef0123456789abcdef0123456789abcdef0123456789abcdef",
			ok:   true,
		},
		{ref: "ghcr.io/org/repo:v1.0.0", ok: false},
	}
	for _, tc := range cases {
		b, err := digestHexBytes(tc.ref)
		if tc.ok && err != nil {
			t.Fatalf("digestHexBytes(%q): unexpected error: %v", tc.ref, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("digestHexBytes(%q): expected error, got bytes", tc.ref)
		}
		if tc.ok && len(b) == 0 {
			t.Fatalf("digestHexBytes(%q): got empty bytes", tc.ref)
		}
	}
}
