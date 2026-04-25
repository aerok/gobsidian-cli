package livesync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

const (
	hkdfPrefix             = "%="
	encryptedMetaPrefix    = `/\:`
	obfuscatedIDPrefix     = "f:"
	encryptedChunkIDPrefix = "h:+"
	encryptedChunkSaltOfID = "a83hrf7f\u0003y7sa8g31"
	pbkdf2Iterations       = 310000
	hkdfSaltLength         = 32
	aesGCMIVLength         = 12
	encryptedEdenChunkKey  = "h:++encrypted-hkdf"
)

type CodecOptions struct {
	Passphrase          string
	PBKDF2Salt          []byte
	PropertyObfuscation bool
}

type Codec struct {
	opts CodecOptions
}

func NewCodec(opts CodecOptions) Codec {
	return Codec{opts: opts}
}

func (c Codec) Enabled() bool {
	return c.opts.Passphrase != ""
}

func (c Codec) DecodeRecords(records []Record) ([]Record, error) {
	if !c.Enabled() {
		return records, nil
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		switch {
		case record.Chunk != nil:
			chunk, err := c.DecodeChunk(*record.Chunk)
			if err != nil {
				return nil, err
			}
			out = append(out, Record{Chunk: &chunk})
		case record.Document != nil:
			doc, err := c.DecodeDocument(*record.Document)
			if err != nil {
				return nil, err
			}
			out = append(out, Record{Document: &doc})
		default:
			out = append(out, record)
		}
	}
	return out, nil
}

func (c Codec) DecodeChunk(chunk Chunk) (Chunk, error) {
	if !c.Enabled() || !strings.HasPrefix(chunk.ID, encryptedChunkIDPrefix) {
		return chunk, nil
	}
	plain, err := DecryptHKDF(chunk.Data, c.opts.Passphrase, c.opts.PBKDF2Salt)
	if err != nil {
		return Chunk{}, fmt.Errorf("decrypt chunk %s: %w", chunk.ID, err)
	}
	chunk.Data = plain
	chunk.Encrypted = false
	return chunk, nil
}

func (c Codec) DecodeDocument(doc Document) (Document, error) {
	if !c.Enabled() {
		return doc, nil
	}
	if strings.HasPrefix(doc.ID, obfuscatedIDPrefix) {
		if !strings.HasPrefix(doc.Path, encryptedMetaPrefix+hkdfPrefix) {
			return Document{}, fmt.Errorf("obfuscated document %s has unsupported metadata path %q", doc.ID, doc.Path)
		}
		plain, err := DecryptHKDF(strings.TrimPrefix(doc.Path, encryptedMetaPrefix), c.opts.Passphrase, c.opts.PBKDF2Salt)
		if err != nil {
			return Document{}, fmt.Errorf("decrypt metadata %s: %w", doc.ID, err)
		}
		var meta encryptedMetadata
		if err := json.Unmarshal([]byte(plain), &meta); err != nil {
			return Document{}, fmt.Errorf("decode metadata %s: %w", doc.ID, err)
		}
		doc.Path = meta.Path
		doc.Mtime = meta.Mtime
		doc.Ctime = meta.Ctime
		doc.Size = meta.Size
		doc.Children = meta.Children
	}
	if encrypted, ok := doc.Eden[encryptedEdenChunkKey]; ok {
		plain, err := DecryptHKDF(encrypted.Data, c.opts.Passphrase, c.opts.PBKDF2Salt)
		if err != nil {
			return Document{}, fmt.Errorf("decrypt eden %s: %w", doc.ID, err)
		}
		var eden map[string]EdenChunk
		if err := json.Unmarshal([]byte(plain), &eden); err != nil {
			return Document{}, fmt.Errorf("decode eden %s: %w", doc.ID, err)
		}
		doc.Eden = eden
	}
	return doc, nil
}

func (c Codec) EncodeChunk(content string) (Chunk, error) {
	if !c.Enabled() {
		return Chunk{ID: "h:" + PlainChunkHash(content), Data: content}, nil
	}
	encrypted, err := EncryptHKDF(content, c.opts.Passphrase, c.opts.PBKDF2Salt)
	if err != nil {
		return Chunk{}, err
	}
	return Chunk{ID: EncryptedChunkID(content, c.opts.Passphrase), Data: encrypted, Encrypted: true}, nil
}

func (c Codec) EncodeDocument(doc Document) (Document, error) {
	if !c.Enabled() {
		return doc, nil
	}
	if c.opts.PropertyObfuscation {
		meta := encryptedMetadata{
			Path:     doc.Path,
			Mtime:    doc.Mtime,
			Ctime:    doc.Ctime,
			Size:     doc.Size,
			Children: doc.Children,
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			return Document{}, err
		}
		encrypted, err := EncryptHKDF(string(raw), c.opts.Passphrase, c.opts.PBKDF2Salt)
		if err != nil {
			return Document{}, err
		}
		doc.ID = PathToID(doc.Path, c.opts.Passphrase)
		doc.Path = encryptedMetaPrefix + encrypted
		doc.Ctime = 0
		doc.Mtime = 0
		doc.Size = 0
		doc.Children = []string{}
	}
	return doc, nil
}

type encryptedMetadata struct {
	Path     string   `json:"path"`
	Mtime    int64    `json:"mtime"`
	Ctime    int64    `json:"ctime"`
	Size     int64    `json:"size"`
	Children []string `json:"children"`
}

func DecryptHKDF(input, passphrase string, pbkdf2Salt []byte) (string, error) {
	if !strings.HasPrefix(input, hkdfPrefix) {
		return "", fmt.Errorf("unsupported encrypted payload prefix")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(input, hkdfPrefix))
	if err != nil {
		return "", err
	}
	if len(raw) < aesGCMIVLength+hkdfSaltLength {
		return "", fmt.Errorf("encrypted payload too short")
	}
	iv := raw[:aesGCMIVLength]
	hkdfSalt := raw[aesGCMIVLength : aesGCMIVLength+hkdfSaltLength]
	ciphertext := raw[aesGCMIVLength+hkdfSaltLength:]
	gcm, err := newGCM(passphrase, pbkdf2Salt, hkdfSalt)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func EncryptHKDF(input, passphrase string, pbkdf2Salt []byte) (string, error) {
	iv := make([]byte, aesGCMIVLength)
	hkdfSalt := make([]byte, hkdfSaltLength)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(rand.Reader, hkdfSalt); err != nil {
		return "", err
	}
	gcm, err := newGCM(passphrase, pbkdf2Salt, hkdfSalt)
	if err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, iv, []byte(input), nil)
	raw := make([]byte, 0, len(iv)+len(hkdfSalt)+len(ciphertext))
	raw = append(raw, iv...)
	raw = append(raw, hkdfSalt...)
	raw = append(raw, ciphertext...)
	return hkdfPrefix + base64.StdEncoding.EncodeToString(raw), nil
}

func newGCM(passphrase string, pbkdf2Salt, hkdfSalt []byte) (cipher.AEAD, error) {
	if len(pbkdf2Salt) == 0 {
		return nil, fmt.Errorf("missing LiveSync PBKDF2 salt")
	}
	master := pbkdf2.Key([]byte(passphrase), pbkdf2Salt, pbkdf2Iterations, 32, sha256.New)
	reader := hkdf.New(sha256.New, master, hkdfSalt, []byte{})
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func EncryptedChunkID(content, passphrase string) string {
	usingLetters := jsStringLength(passphrase) * 3 / 4
	prefix := jsSubstringByCodeUnits(passphrase, usingLetters)
	hashedPassphrase := fallbackMixedHashEach(encryptedChunkSaltOfID + prefix)
	value := content + "-" + hashedPassphrase + "-" + strconv.Itoa(jsStringLength(content))
	return encryptedChunkIDPrefix + strconv.FormatUint(xxhash.Sum64String(value), 36)
}

func PathToID(filename, passphrase string) string {
	if strings.HasPrefix(filename, "_") {
		filename = "/" + filename
	}
	if passphrase == "" {
		return filename
	}
	hashedPassphrase := sha256Hex(passphrase)
	return obfuscatedIDPrefix + sha256Hex(hashedPassphrase+":"+filename)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func jsStringLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func jsSubstringByCodeUnits(value string, codeUnits int) string {
	if codeUnits <= 0 {
		return ""
	}
	units := utf16.Encode([]rune(value))
	if codeUnits > len(units) {
		codeUnits = len(units)
	}
	return string(utf16.Decode(units[:codeUnits]))
}

const epochFNV1a uint32 = 2166136261

func fallbackMixedHashEach(src string) string {
	m, f := mixedHash(strconv.Itoa(jsStringLength(src))+src, 1, epochFNV1a)
	return strconv.FormatUint(uint64(m), 36) + strconv.FormatUint(uint64(f), 36)
}

func mixedHash(str string, seed, fnv uint32) (uint32, uint32) {
	const (
		c1 uint32 = 0xcc9e2d51
		c2 uint32 = 0x1b873593
		r1        = 15
		r2        = 13
		m  uint32 = 5
		n  uint32 = 0xe6546b64
	)
	h1 := float64(seed)
	fnv1aHash := fnv
	for _, k1 := range utf16.Encode([]rune(str)) {
		k := float64(k1)
		fnv1aHash ^= uint32(k1)
		fnv1aHash = uint32(int32(fnv1aHash) * int32(0x01000193))
		k *= float64(c1)
		k = float64(int32((jsToUint32(k) << r1) | (jsToUint32(k) >> (32 - r1))))
		k *= float64(c2)
		h1 = float64(jsToInt32(h1) ^ jsToInt32(k))
		h1 = float64(int32((uint32(jsToInt32(h1)) << r2) | (uint32(jsToInt32(h1)) >> (32 - r2))))
		h1 = h1*float64(m) + float64(n)
	}
	h := jsToInt32(h1) ^ int32(jsStringLength(str))
	h ^= int32(uint32(h) >> 16)
	h = int32(uint32(h) * 0x85ebca6b)
	h ^= int32(uint32(h) >> 13)
	h = int32(uint32(h) * 0xc2b2ae35)
	h ^= int32(uint32(h) >> 16)
	return uint32(h), fnv1aHash
}

func jsToUint32(value float64) uint32 {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	value = math.Mod(math.Trunc(value), 4294967296)
	if value < 0 {
		value += 4294967296
	}
	return uint32(value)
}

func jsToInt32(value float64) int32 {
	unsigned := jsToUint32(value)
	return int32(unsigned)
}
