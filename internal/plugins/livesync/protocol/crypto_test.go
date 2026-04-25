package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecryptHKDFMatchesLiveSyncFixture(t *testing.T) {
	salt := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	got, err := DecryptHKDF("%=2ddutJwgMpXQlzFu2rWmkY+TBYd+vxpRI+jH3CPZOHBi2oBrfBfsk/VFXfpbW2L3IusvpHqGYf9LcLWxNulfD2GtyG6QkYIuc55Eog==", "secret-pass", salt)
	if err != nil {
		t.Fatalf("DecryptHKDF returned error: %v", err)
	}
	if got != "hello encrypted\n" {
		t.Fatalf("unexpected plaintext: %q", got)
	}
}

func TestDecryptEncryptedMetadataPath(t *testing.T) {
	salt := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	doc := Document{
		ID:   "f:fixture",
		Path: `/\:%=6U6h8BFVlSp77qa6FAvVQqeJ3LRxfuDtwsphI5SNdYH9xA7lP7m24JCaHRwVGEiCa++aeNAzSzqK0AgbNWcFE6rTJ0utK8mEK14Mw8LMOWWpE226bFmZVrI8oTN0St0CFZuAZBBeGD8TVbk/k90+7Tx2wydd8os/1zTqpkjRpu+YyjnLjcw868uGzaZJ`,
		Type: "plain",
	}
	codec := NewCodec(CodecOptions{Passphrase: "secret-pass", PBKDF2Salt: salt, PropertyObfuscation: true})
	decoded, err := codec.DecodeDocument(doc)
	if err != nil {
		t.Fatalf("DecodeDocument returned error: %v", err)
	}
	if decoded.Path != "secret/note.md" || decoded.Mtime != 123 || decoded.Ctime != 100 || decoded.Size != 16 {
		t.Fatalf("unexpected decoded metadata: %#v", decoded)
	}
	if len(decoded.Children) != 1 || decoded.Children[0] != "h:+abc" {
		t.Fatalf("unexpected decoded children: %#v", decoded.Children)
	}
}

func TestEncryptedChunkIDMatchesLiveSyncFixture(t *testing.T) {
	got := EncryptedChunkID("hello encrypted\n", "secret-pass")
	if got != "h:+11r30enaj9z36" {
		t.Fatalf("unexpected encrypted chunk id: %s", got)
	}
}

func TestPathToIDUsesLiveSyncCaseInsensitiveHash(t *testing.T) {
	got := PathToID("Mixed/Case-Note.md", "test-passphrase", false)
	want := "f:662c25bea9c22977cd571b14b1e9ea058dbd13cbb7de363ec7d62ac7a77005e4"
	if got != want {
		t.Fatalf("unexpected path id: got %s want %s", got, want)
	}
}

func TestPathToIDCanUseCaseSensitiveHash(t *testing.T) {
	insensitive := PathToID("Mixed/Case-Note.md", "test-passphrase", false)
	sensitive := PathToID("Mixed/Case-Note.md", "test-passphrase", true)
	if sensitive == insensitive {
		t.Fatalf("case-sensitive path id should preserve path casing")
	}
	if sensitive != "f:4b415ed7f703ac262773c6159feb99c8a8caa50444117461c31273c574681293" {
		t.Fatalf("unexpected case-sensitive path id: %s", sensitive)
	}
}

func TestEncryptHKDFRoundTripUsesLiveSyncPrefix(t *testing.T) {
	salt := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	encrypted, err := EncryptHKDF("round trip", "secret-pass", salt)
	if err != nil {
		t.Fatalf("EncryptHKDF returned error: %v", err)
	}
	if !strings.HasPrefix(encrypted, "%=") || strings.Contains(encrypted, "round trip") {
		t.Fatalf("unexpected ciphertext: %q", encrypted)
	}
	plain, err := DecryptHKDF(encrypted, "secret-pass", salt)
	if err != nil {
		t.Fatalf("DecryptHKDF returned error: %v", err)
	}
	if plain != "round trip" {
		t.Fatalf("unexpected round trip value: %q", plain)
	}
}

func TestEncodeDocumentKeepsEncryptedMetadataStubFields(t *testing.T) {
	salt := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	codec := NewCodec(CodecOptions{Passphrase: "secret-pass", PBKDF2Salt: salt, PropertyObfuscation: true})
	doc, err := codec.EncodeDocument(Document{
		ID:       "note.md",
		Path:     "note.md",
		Ctime:    100,
		Mtime:    123,
		Size:     16,
		Type:     "plain",
		Children: []string{"h:+abc"},
		Eden:     map[string]EdenChunk{},
	})
	if err != nil {
		t.Fatalf("EncodeDocument returned error: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	for _, key := range []string{"children", "ctime", "mtime", "size"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("encoded obfuscated document should retain top-level %q field: %s", key, raw)
		}
	}
	if got["ctime"] != float64(0) || got["mtime"] != float64(0) || got["size"] != float64(0) {
		t.Fatalf("metadata stub fields should be zeroed, got %s", raw)
	}
	if children, ok := got["children"].([]any); !ok || len(children) != 0 {
		t.Fatalf("children should be present as an empty array, got %s", raw)
	}
}
