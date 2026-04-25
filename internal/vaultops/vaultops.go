package vaultops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const stateDir = ".gobsidian"

type Entry struct {
	Path  string   `json:"path"`
	Type  string   `json:"type"`
	Size  int64    `json:"size"`
	Mtime int64    `json:"mtime"`
	Title string   `json:"title,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

type SearchOptions struct {
	Query         string
	TitleQuery    string
	Tags          []string
	Limit         int
	Offset        int
	CaseSensitive bool
	IncludeHidden bool
}

type SearchResult struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Snippets []string `json:"snippets,omitempty"`
	Score    int      `json:"score"`
	Mtime    int64    `json:"mtime"`
	Size     int64    `json:"size"`
}

type SearchResponse struct {
	OK      bool           `json:"ok"`
	Command string         `json:"command"`
	Vault   string         `json:"vault"`
	Query   string         `json:"query,omitempty"`
	Title   string         `json:"title,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
	Total   int            `json:"total"`
	Results []SearchResult `json:"results"`
}

type ListOptions struct {
	Folder        string
	Recursive     bool
	Depth         int
	Type          string
	IncludeHidden bool
}

type ListResponse struct {
	OK      bool    `json:"ok"`
	Command string  `json:"command"`
	Vault   string  `json:"vault"`
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

type ReadOptions struct {
	Ref      string
	JSON     bool
	Head     int
	Tail     int
	Range    string
	MaxBytes int
}

type ReadResult struct {
	OK        bool     `json:"ok"`
	Command   string   `json:"command"`
	Vault     string   `json:"vault"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags,omitempty"`
	Size      int64    `json:"size"`
	Mtime     int64    `json:"mtime"`
	Content   string   `json:"content"`
	Truncated bool     `json:"truncated"`
}

type MoveOptions struct {
	From        string
	To          string
	Overwrite   bool
	DryRun      bool
	UpdateLinks bool
}

type MoveResponse struct {
	OK           bool         `json:"ok"`
	Command      string       `json:"command"`
	Vault        string       `json:"vault"`
	From         string       `json:"from"`
	To           string       `json:"to"`
	DryRun       bool         `json:"dry_run"`
	UpdatedLinks []LinkUpdate `json:"updated_links,omitempty"`
}

type LinkUpdate struct {
	Path         string `json:"path"`
	Replacements int    `json:"replacements"`
}

type linkUpdatePlan struct {
	Path         string
	Content      string
	Replacements int
}

type FrontmatterResponse struct {
	OK          bool           `json:"ok"`
	Command     string         `json:"command"`
	Vault       string         `json:"vault"`
	Path        string         `json:"path"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Key         string         `json:"key,omitempty"`
	Value       any            `json:"value,omitempty"`
	Changed     bool           `json:"changed,omitempty"`
}

func Search(root, vaultName string, opts SearchOptions) (SearchResponse, error) {
	if opts.Offset < 0 {
		return SearchResponse{}, fmt.Errorf("offset must be greater than or equal to 0")
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	files, err := markdownFiles(root, opts.IncludeHidden, true)
	if err != nil {
		return SearchResponse{}, err
	}
	var results []SearchResult
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return SearchResponse{}, err
		}
		fm, body, err := ParseFrontmatter(string(data))
		if err != nil {
			if !errors.Is(err, ErrNoFrontmatter) {
				return SearchResponse{}, fmt.Errorf("%s: %w", file.Path, err)
			}
			fm = map[string]any{}
			body = string(data)
		}
		tags := Tags(fm, body)
		if !hasAllTags(tags, opts.Tags) {
			continue
		}
		title := noteTitle(file.Path)
		score := 0
		var snippets []string
		if opts.TitleQuery != "" {
			if !contains(title, opts.TitleQuery, opts.CaseSensitive) && !contains(file.Path, opts.TitleQuery, opts.CaseSensitive) {
				continue
			}
			score += 100 + occurrences(title+" "+file.Path, opts.TitleQuery, opts.CaseSensitive)
		}
		if opts.Query != "" {
			count := occurrences(body, opts.Query, opts.CaseSensitive)
			if count == 0 {
				continue
			}
			score += count
			snippets = makeSnippets(body, opts.Query, opts.CaseSensitive, 3)
		}
		if opts.Query == "" && opts.TitleQuery == "" {
			score += 1
		}
		results = append(results, SearchResult{
			Path:     file.Path,
			Title:    title,
			Tags:     tags,
			Snippets: snippets,
			Score:    score,
			Mtime:    file.Mtime,
			Size:     file.Size,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Mtime != results[j].Mtime {
			return results[i].Mtime > results[j].Mtime
		}
		return results[i].Path < results[j].Path
	})
	total := len(results)
	if opts.Offset > total {
		results = nil
	} else {
		results = results[opts.Offset:]
	}
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return SearchResponse{OK: true, Command: "search", Vault: vaultName, Query: opts.Query, Title: opts.TitleQuery, Tags: opts.Tags, Total: total, Results: results}, nil
}

func List(root, vaultName string, opts ListOptions) (ListResponse, error) {
	if opts.Type == "" {
		opts.Type = "all"
	}
	base, rel, err := safePath(root, opts.Folder)
	if err != nil {
		return ListResponse{}, err
	}
	info, err := os.Stat(base)
	if err != nil {
		return ListResponse{}, err
	}
	if !info.IsDir() {
		return ListResponse{}, fmt.Errorf("%q is not a directory", rel)
	}
	excluded := excludedPaths(root)
	var entries []Entry
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == base {
			return nil
		}
		itemRel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		itemRel = filepath.ToSlash(itemRel)
		if shouldSkip(itemRel, entry.IsDir(), opts.IncludeHidden, excluded) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		localRel, _ := filepath.Rel(base, path)
		depth := pathDepth(filepath.ToSlash(localRel))
		if !opts.Recursive && depth > 1 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if opts.Depth > 0 && depth > opts.Depth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		typ := entryType(entry)
		if !typeMatches(typ, opts.Type) {
			return nil
		}
		out := Entry{Path: itemRel, Type: typ, Size: info.Size(), Mtime: info.ModTime().UnixMilli()}
		if typ == "note" {
			out.Title = noteTitle(itemRel)
			if data, err := os.ReadFile(path); err == nil {
				fm, body, err := ParseFrontmatter(string(data))
				if err != nil {
					if !errors.Is(err, ErrNoFrontmatter) {
						return fmt.Errorf("%s: %w", itemRel, err)
					}
					fm = map[string]any{}
					body = string(data)
				}
				out.Tags = Tags(fm, body)
			}
		}
		entries = append(entries, out)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "dir"
		}
		return entries[i].Path < entries[j].Path
	})
	return ListResponse{OK: true, Command: "list", Vault: vaultName, Path: rel, Entries: entries}, err
}

func Read(root, vaultName string, opts ReadOptions) (ReadResult, error) {
	resolved, err := ResolveNote(root, opts.Ref, false)
	if err != nil {
		return ReadResult{}, err
	}
	full := filepath.Join(root, filepath.FromSlash(resolved))
	data, err := os.ReadFile(full)
	if err != nil {
		return ReadResult{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return ReadResult{}, err
	}
	content, truncated, err := sliceContent(string(data), opts)
	if err != nil {
		return ReadResult{}, err
	}
	fm, body, err := ParseFrontmatter(string(data))
	if err != nil {
		if !errors.Is(err, ErrNoFrontmatter) {
			return ReadResult{}, err
		}
		fm = map[string]any{}
		body = string(data)
	}
	return ReadResult{OK: true, Command: "read", Vault: vaultName, Path: resolved, Title: noteTitle(resolved), Tags: Tags(fm, body), Size: info.Size(), Mtime: info.ModTime().UnixMilli(), Content: content, Truncated: truncated}, nil
}

func Move(root, vaultName string, opts MoveOptions) (MoveResponse, error) {
	fromRel, err := ResolveNote(root, opts.From, false)
	if err != nil {
		return MoveResponse{}, err
	}
	toRel := ensureMarkdownExt(filepath.ToSlash(opts.To))
	toFull, toClean, err := safePath(root, toRel)
	if err != nil {
		return MoveResponse{}, err
	}
	if fromRel == toClean {
		return MoveResponse{}, fmt.Errorf("source and destination are the same")
	}
	fromFull := filepath.Join(root, filepath.FromSlash(fromRel))
	toInfo, statErr := os.Stat(toFull)
	if statErr == nil {
		if toInfo.IsDir() {
			return MoveResponse{}, fmt.Errorf("destination %q is a directory", toClean)
		}
		if !opts.Overwrite {
			return MoveResponse{}, fmt.Errorf("destination %q already exists", toClean)
		}
	} else if !os.IsNotExist(statErr) {
		return MoveResponse{}, statErr
	}
	var updates []LinkUpdate
	var plans []linkUpdatePlan
	if opts.UpdateLinks {
		var err error
		plans, err = planLinkUpdates(root, fromRel, toClean)
		if err != nil {
			return MoveResponse{}, err
		}
		updates = linkUpdates(plans)
	}
	if !opts.DryRun {
		if err := os.MkdirAll(filepath.Dir(toFull), 0o755); err != nil {
			return MoveResponse{}, err
		}
		var backup string
		if statErr == nil && opts.Overwrite {
			var err error
			backup, err = backupDestination(toFull)
			if err != nil {
				return MoveResponse{}, err
			}
		}
		if err := os.Rename(fromFull, toFull); err != nil {
			if backup != "" {
				_ = os.Rename(backup, toFull)
			}
			return MoveResponse{}, err
		}
		if backup != "" {
			_ = os.Remove(backup)
		}
		if opts.UpdateLinks {
			if err := applyLinkUpdates(root, plans); err != nil {
				return MoveResponse{}, fmt.Errorf("note moved to %q, but link updates failed: %w", toClean, err)
			}
		}
	}
	return MoveResponse{OK: true, Command: "move", Vault: vaultName, From: fromRel, To: toClean, DryRun: opts.DryRun, UpdatedLinks: updates}, nil
}

func FrontmatterGet(root, vaultName, ref, key string) (FrontmatterResponse, error) {
	rel, content, err := readNoteContent(root, ref)
	if err != nil {
		return FrontmatterResponse{}, err
	}
	fm, _, err := ParseFrontmatter(content)
	if err != nil && !errors.Is(err, ErrNoFrontmatter) {
		return FrontmatterResponse{}, err
	}
	resp := FrontmatterResponse{OK: true, Command: "frontmatter get", Vault: vaultName, Path: rel, Frontmatter: fm, Key: key}
	if key != "" {
		resp.Value = fm[key]
	}
	return resp, nil
}

func FrontmatterSet(root, vaultName, ref, key, rawValue string) (FrontmatterResponse, error) {
	rel, content, err := readNoteContent(root, ref)
	if err != nil {
		return FrontmatterResponse{}, err
	}
	value, err := parseYAMLValue(rawValue)
	if err != nil {
		return FrontmatterResponse{}, err
	}
	updated, fm, err := SetFrontmatterKey(content, key, value)
	if err != nil {
		return FrontmatterResponse{}, err
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(updated), 0o644); err != nil {
		return FrontmatterResponse{}, err
	}
	return FrontmatterResponse{OK: true, Command: "frontmatter set", Vault: vaultName, Path: rel, Frontmatter: fm, Key: key, Value: value, Changed: true}, nil
}

func FrontmatterDelete(root, vaultName, ref, key string) (FrontmatterResponse, error) {
	rel, content, err := readNoteContent(root, ref)
	if err != nil {
		return FrontmatterResponse{}, err
	}
	updated, fm, changed, err := DeleteFrontmatterKey(content, key)
	if err != nil {
		return FrontmatterResponse{}, err
	}
	if changed {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(updated), 0o644); err != nil {
			return FrontmatterResponse{}, err
		}
	}
	return FrontmatterResponse{OK: true, Command: "frontmatter delete", Vault: vaultName, Path: rel, Frontmatter: fm, Key: key, Changed: changed}, nil
}

var ErrNoFrontmatter = errors.New("note does not contain frontmatter")

func ParseFrontmatter(content string) (map[string]any, string, error) {
	if content == "---" {
		return map[string]any{}, content, fmt.Errorf("frontmatter is missing closing delimiter")
	}
	openLen := 0
	switch {
	case strings.HasPrefix(content, "---\r\n"):
		openLen = len("---\r\n")
	case strings.HasPrefix(content, "---\n"):
		openLen = len("---\n")
	default:
		return map[string]any{}, content, ErrNoFrontmatter
	}
	rest := content[openLen:]
	idx, delimiterLen := frontmatterDelimiter(rest)
	if idx < 0 {
		return map[string]any{}, content, fmt.Errorf("frontmatter is missing closing delimiter")
	}
	raw := rest[:idx]
	body := rest[idx+delimiterLen:]
	if strings.HasPrefix(body, "\r\n") {
		body = body[2:]
	} else if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	fm := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
			return map[string]any{}, content, fmt.Errorf("frontmatter contains invalid YAML: %w", err)
		}
	}
	return fm, body, nil
}

func frontmatterDelimiter(rest string) (int, int) {
	for i := 0; i < len(rest); i++ {
		delimiterLen := 0
		switch {
		case strings.HasPrefix(rest[i:], "\r\n---"):
			delimiterLen = len("\r\n---")
		case strings.HasPrefix(rest[i:], "\n---"):
			delimiterLen = len("\n---")
		default:
			continue
		}
		after := i + delimiterLen
		if after == len(rest) || rest[after] == '\n' || rest[after] == '\r' {
			return i, delimiterLen
		}
	}
	return -1, 0
}

func SetFrontmatterKey(content, key string, value any) (string, map[string]any, error) {
	fm, body, err := ParseFrontmatter(content)
	if err != nil {
		if !errors.Is(err, ErrNoFrontmatter) {
			return "", nil, err
		}
		fm = map[string]any{}
		body = content
	}
	fm[key] = value
	out, err := formatFrontmatter(fm, body)
	return out, fm, err
}

func DeleteFrontmatterKey(content, key string) (string, map[string]any, bool, error) {
	fm, body, err := ParseFrontmatter(content)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			return content, map[string]any{}, false, nil
		}
		return "", nil, false, err
	}
	if _, ok := fm[key]; !ok {
		return content, fm, false, nil
	}
	delete(fm, key)
	if len(fm) == 0 {
		return body, fm, true, nil
	}
	out, err := formatFrontmatter(fm, body)
	return out, fm, true, err
}

func Tags(fm map[string]any, body string) []string {
	seen := map[string]bool{}
	var tags []string
	add := func(tag string) {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")
		if tag == "" || seen[tag] {
			return
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	if raw, ok := fm["tags"]; ok {
		addFrontmatterTags(raw, add)
	}
	for _, tag := range inlineTags(body) {
		add(tag)
	}
	sort.Strings(tags)
	return tags
}

func ResolveNote(root, ref string, includeHidden bool) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("note path is required")
	}
	candidates := []string{filepath.ToSlash(ref)}
	if filepath.Ext(ref) == "" {
		candidates = append(candidates, filepath.ToSlash(ref)+".md")
	}
	for _, candidate := range candidates {
		full, clean, err := safePath(root, candidate)
		if err != nil {
			return "", err
		}
		if shouldSkip(clean, false, includeHidden, nil) {
			continue
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return clean, nil
		}
	}
	files, err := markdownFiles(root, includeHidden, false)
	if err != nil {
		return "", err
	}
	refTitle := strings.TrimSuffix(filepath.Base(filepath.ToSlash(ref)), filepath.Ext(ref))
	var matches []string
	for _, file := range files {
		if noteTitle(file.Path) == refTitle || strings.EqualFold(noteTitle(file.Path), refTitle) {
			matches = append(matches, file.Path)
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous note %q; candidates: %s", ref, strings.Join(matches, ", "))
	}
	return "", fmt.Errorf("note %q not found", ref)
}

func readNoteContent(root, ref string) (string, string, error) {
	rel, err := ResolveNote(root, ref, false)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", "", err
	}
	return rel, string(data), nil
}

func markdownFiles(root string, includeHidden bool, respectExcluded bool) ([]Entry, error) {
	var excluded []string
	if respectExcluded {
		excluded = excludedPaths(root)
	}
	var files []Entry
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
		if shouldSkip(rel, entry.IsDir(), includeHidden, excluded) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(rel), ".md") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, Entry{Path: rel, Type: "note", Size: info.Size(), Mtime: info.ModTime().UnixMilli(), Title: noteTitle(rel)})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func safePath(root, rel string) (string, string, error) {
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("unsafe vault path %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." {
		return root, ".", nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("unsafe vault path %q", rel)
	}
	cleanSlash := filepath.ToSlash(clean)
	return filepath.Join(root, clean), cleanSlash, nil
}

func shouldSkip(rel string, isDir, includeHidden bool, excluded []string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == stateDir {
			return true
		}
		if !includeHidden && strings.HasPrefix(part, ".") {
			return true
		}
	}
	if len(excluded) > 0 && isExcluded(rel, excluded) {
		return true
	}
	return false
}

func entryType(entry fs.DirEntry) string {
	if entry.IsDir() {
		return "dir"
	}
	if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
		return "note"
	}
	return "file"
}

func typeMatches(actual, want string) bool {
	switch want {
	case "", "all":
		return true
	case "note":
		return actual == "note"
	case "file":
		return actual == "file"
	case "dir":
		return actual == "dir"
	default:
		return false
	}
}

type appConfig struct {
	UserIgnoreFilters []string `json:"userIgnoreFilters"`
}

func excludedPaths(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".obsidian", "app.json"))
	if err != nil {
		return nil
	}
	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return cfg.UserIgnoreFilters
}

func isExcluded(rel string, filters []string) bool {
	normalized := filepath.ToSlash(rel)
	for _, filter := range filters {
		filter = strings.TrimSpace(strings.TrimRight(filepath.ToSlash(filter), "/"))
		if filter == "" {
			continue
		}
		if !strings.ContainsAny(filter, "*?[") {
			if normalized == filter || strings.HasPrefix(normalized, filter+"/") {
				return true
			}
			continue
		}
		if strings.HasPrefix(filter, "**/") {
			if matchPathOrSegments(normalized, strings.TrimPrefix(filter, "**/")) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(filter, normalized); ok {
			return true
		}
		if matchPathSegments(normalized, filter) {
			return true
		}
	}
	return false
}

func matchPathOrSegments(rel, pattern string) bool {
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		sub := strings.Join(parts[i:], "/")
		if ok, _ := filepath.Match(pattern, sub); ok {
			return true
		}
	}
	return matchPathSegments(rel, pattern)
}

func matchPathSegments(rel, pattern string) bool {
	for _, part := range strings.Split(rel, "/") {
		if ok, _ := filepath.Match(pattern, part); ok {
			return true
		}
	}
	return false
}

func pathDepth(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return len(strings.Split(rel, "/"))
}

func noteTitle(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func ensureMarkdownExt(path string) string {
	if filepath.Ext(path) == "" {
		return path + ".md"
	}
	return path
}

func contains(s, sub string, caseSensitive bool) bool {
	return occurrences(s, sub, caseSensitive) > 0
}

func occurrences(s, sub string, caseSensitive bool) int {
	if sub == "" {
		return 0
	}
	if !caseSensitive {
		s = strings.ToLower(s)
		sub = strings.ToLower(sub)
	}
	return strings.Count(s, sub)
}

func hasAllTags(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, tag := range have {
		set[strings.ToLower(strings.TrimPrefix(tag, "#"))] = true
	}
	for _, tag := range want {
		if !set[strings.ToLower(strings.TrimPrefix(tag, "#"))] {
			return false
		}
	}
	return true
}

func makeSnippets(body, query string, caseSensitive bool, max int) []string {
	if query == "" {
		return nil
	}
	var out []string
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if contains(line, query, caseSensitive) {
			out = append(out, strings.TrimSpace(line))
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func sliceContent(content string, opts ReadOptions) (string, bool, error) {
	if opts.Range != "" {
		parts := strings.Split(opts.Range, ":")
		if len(parts) != 2 {
			return "", false, fmt.Errorf("range must be start:end")
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return "", false, err
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", false, err
		}
		lines := strings.Split(content, "\n")
		if start < 1 {
			start = 1
		}
		if end > len(lines) {
			end = len(lines)
		}
		if end < start {
			return "", true, nil
		}
		return strings.Join(lines[start-1:end], "\n"), true, nil
	}
	if opts.Head > 0 {
		lines := strings.Split(content, "\n")
		if opts.Head >= len(lines) {
			return content, false, nil
		}
		return strings.Join(lines[:opts.Head], "\n"), true, nil
	}
	if opts.Tail > 0 {
		lines := strings.Split(content, "\n")
		if opts.Tail >= len(lines) {
			return content, false, nil
		}
		return strings.Join(lines[len(lines)-opts.Tail:], "\n"), true, nil
	}
	if opts.MaxBytes > 0 && len([]byte(content)) > opts.MaxBytes {
		data := []byte(content)
		cut := opts.MaxBytes
		for cut > 0 && !utf8.Valid(data[:cut]) {
			cut--
		}
		return string(data[:cut]), true, nil
	}
	return content, false, nil
}

func formatFrontmatter(fm map[string]any, body string) (string, error) {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(fm); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	buf.WriteString("---\n")
	buf.WriteString(body)
	return buf.String(), nil
}

func parseYAMLValue(raw string) (any, error) {
	var value any
	if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func addFrontmatterTags(raw any, add func(string)) {
	switch v := raw.(type) {
	case string:
		for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
			add(part)
		}
	case []any:
		for _, item := range v {
			add(fmt.Sprint(item))
		}
	case []string:
		for _, item := range v {
			add(item)
		}
	}
}

func inlineTags(body string) []string {
	var tags []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] != '#' || (i > 0 && isTagRune(rune(line[i-1]))) {
				continue
			}
			j := i + 1
			for j < len(line) {
				r, size := utf8.DecodeRuneInString(line[j:])
				if !isTagRune(r) {
					break
				}
				j += size
			}
			if j > i+1 {
				tags = append(tags, line[i+1:j])
			}
			i = j
		}
	}
	return tags
}

func isTagRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '/'
}

var wikiLinkRE = regexp.MustCompile(`\[\[([^\]|#]+)(#[^\]|]*)?(\|[^\]]*)?\]\]`)
var mdLinkRE = regexp.MustCompile(`\]\(([^)#?]+)(#[^)]+)?\)`)

func updateLinks(root, fromRel, toRel string, dryRun bool) ([]LinkUpdate, error) {
	plans, err := planLinkUpdates(root, fromRel, toRel)
	if err != nil {
		return nil, err
	}
	if !dryRun {
		if err := applyLinkUpdates(root, plans); err != nil {
			return nil, err
		}
	}
	return linkUpdates(plans), nil
}

func planLinkUpdates(root, fromRel, toRel string) ([]linkUpdatePlan, error) {
	files, err := markdownFiles(root, true, false)
	if err != nil {
		return nil, err
	}
	fromNoExt := strings.TrimSuffix(fromRel, filepath.Ext(fromRel))
	fromBase := noteTitle(fromRel)
	toNoExt := strings.TrimSuffix(toRel, filepath.Ext(toRel))
	toBase := noteTitle(toRel)
	var updates []linkUpdatePlan
	for _, file := range files {
		if file.Path == fromRel {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(file.Path))
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		content := string(data)
		replacements := 0
		content = wikiLinkRE.ReplaceAllStringFunc(content, func(match string) string {
			parts := wikiLinkRE.FindStringSubmatch(match)
			target := parts[1]
			if target != fromNoExt && target != fromRel && target != fromBase {
				return match
			}
			replacements++
			newTarget := toBase
			if strings.Contains(target, "/") || target == fromRel {
				newTarget = toNoExt
			}
			return "[[" + newTarget + parts[2] + parts[3] + "]]"
		})
		content = mdLinkRE.ReplaceAllStringFunc(content, func(match string) string {
			parts := mdLinkRE.FindStringSubmatch(match)
			target := filepath.ToSlash(parts[1])
			resolvedTarget := cleanLinkTarget(file.Path, target)
			if resolvedTarget != fromRel && resolvedTarget != fromNoExt && resolvedTarget != fromBase+".md" {
				return match
			}
			replacements++
			return "](" + relativeLinkTarget(file.Path, toRel) + parts[2] + ")"
		})
		if replacements > 0 {
			updates = append(updates, linkUpdatePlan{Path: file.Path, Content: content, Replacements: replacements})
		}
	}
	return updates, nil
}

func applyLinkUpdates(root string, plans []linkUpdatePlan) error {
	for _, plan := range plans {
		full, _, err := safePath(root, plan.Path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(plan.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func linkUpdates(plans []linkUpdatePlan) []LinkUpdate {
	updates := make([]LinkUpdate, 0, len(plans))
	for _, plan := range plans {
		updates = append(updates, LinkUpdate{Path: plan.Path, Replacements: plan.Replacements})
	}
	return updates
}

func backupDestination(path string) (string, error) {
	for i := 0; i < 100; i++ {
		backup := fmt.Sprintf("%s.gobsidian-backup-%d-%d", path, os.Getpid(), i)
		if _, err := os.Stat(backup); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Rename(path, backup); err != nil {
			return "", err
		}
		return backup, nil
	}
	return "", fmt.Errorf("could not create backup for %q", filepath.ToSlash(path))
}

func cleanLinkTarget(fromFile, target string) string {
	if filepath.IsAbs(target) {
		return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(target))), "/")
	}
	base := filepath.Dir(fromFile)
	if base == "." {
		return filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(filepath.FromSlash(base), filepath.FromSlash(target))))
}

func relativeLinkTarget(fromFile, toRel string) string {
	base := filepath.Dir(filepath.FromSlash(fromFile))
	target := filepath.FromSlash(toRel)
	if base == "." {
		return filepath.ToSlash(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}
