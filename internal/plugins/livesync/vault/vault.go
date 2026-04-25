package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const StateDir = ".gobsidian"

type File struct {
	Path    string
	Content []byte
	Hash    string
	Mtime   int64
	Deleted bool
}

func WriteSnapshot(root string, files map[string]File) error {
	for path, file := range files {
		clean, err := safePath(root, path)
		if err != nil {
			return err
		}
		if file.Deleted {
			if err := moveToTrash(root, path, clean); err != nil {
				return err
			}
			continue
		}
		if err := writeFileAtomic(clean, file.Content, 0o644); err != nil {
			return err
		}
		if file.Mtime > 0 {
			mod := time.UnixMilli(file.Mtime)
			_ = os.Chtimes(clean, mod, mod)
		}
	}
	return nil
}

func Scan(root string) (map[string]File, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	files := map[string]File{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if isHiddenPath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[rel] = File{
			Path:    rel,
			Content: data,
			Hash:    hashBytes(data),
			Mtime:   info.ModTime().UnixMilli(),
		}
		return nil
	})
	return files, err
}

func isHiddenPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gobsidian-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safePath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe vault path %q", rel)
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) || cleanRel == ".." {
		return "", fmt.Errorf("unsafe vault path %q", rel)
	}
	full := filepath.Join(root, cleanRel)
	return full, nil
}

func moveToTrash(root, rel, src string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	trashRel := fmt.Sprintf("%d-%s", time.Now().UnixNano(), strings.ReplaceAll(rel, "/", "__"))
	dst := filepath.Join(root, StateDir, "trash", trashRel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}
