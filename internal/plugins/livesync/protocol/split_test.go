package protocol

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestSplitTextRabinKarpMatchesLiveSyncV3Fixture(t *testing.T) {
	content := "# GUI Upload Probe\n\n- created_at_utc: 20260425T063500Z\n- purpose: verify Obsidian GUI writes to the same LiveSync CouchDB\n- source: local gui vault probe\n"

	got := SplitTextRabinKarp(content)
	want := []string{
		"# GUI Upload Probe\n\n- created_at_utc: 20260425T063500Z\n- purpose: verify Obsidian GUI writes to the same LiveSync CouchDB\n- source: local gui",
		" vault probe\n",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitTextRabinKarp() = %#v, want %#v", got, want)
	}
}

func TestSplitTextRabinKarpKeepsUTF8Boundaries(t *testing.T) {
	content := "标题\n\n这是一些中文内容，用于确认分块不会切断 UTF-8 字符。\n"
	for _, piece := range SplitTextRabinKarp(content) {
		if !utf8.ValidString(piece) {
			t.Fatalf("piece is not valid UTF-8: %q", piece)
		}
	}
}
