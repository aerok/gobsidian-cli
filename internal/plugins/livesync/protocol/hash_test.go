package protocol

import "testing"

func TestLiveSyncPlainChunkHashMatchesCommonlib(t *testing.T) {
	cases := map[string]string{
		"test3\n": "18we5dn8bf6rz",
		"fasd\n":  "2sho5i52uv1xn",
		"fasfa":   "3lkkqdkcfgdnd",
	}
	for input, want := range cases {
		if got := PlainChunkHash(input); got != want {
			t.Fatalf("PlainChunkHash(%q) = %q, want %q", input, got, want)
		}
	}
}
