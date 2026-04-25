package syncer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type State struct {
	CouchSince string               `json:"couch_since"`
	Files      map[string]FileState `json:"files"`
	LastSync   int64                `json:"last_sync,omitempty"`
	LastError  string               `json:"last_error,omitempty"`
}

type FileState struct {
	Hash      string `json:"hash"`
	DocID     string `json:"doc_id,omitempty"`
	RemoteRev string `json:"remote_rev,omitempty"`
	Mtime     int64  `json:"mtime,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

func (s State) Clone() State {
	out := s
	out.Files = map[string]FileState{}
	for path, file := range s.Files {
		out.Files[path] = file
	}
	return out
}

func acquireStateLock(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func releaseStateLock(lock *os.File) {
	if lock == nil {
		return
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func LoadState(path string) (State, error) {
	state := State{Files: map[string]FileState{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.Files == nil {
		state.Files = map[string]FileState{}
	}
	return state, nil
}

func SaveState(path string, state State) error {
	if state.Files == nil {
		state.Files = map[string]FileState{}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*")
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
	if err := tmp.Chmod(0o600); err != nil {
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
