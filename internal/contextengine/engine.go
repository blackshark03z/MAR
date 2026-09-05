package contextengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"mar/internal/domain"
)

var ErrRevisionMismatch = errors.New("context snapshot revision does not match expected revision")

type Repository interface {
	Snapshot(context.Context, string) (RepositorySnapshot, error)
}

type RepositorySnapshot struct {
	Revision string
	Files    []RepositoryFile
}

type RepositoryFile struct {
	Path   string
	Status string
}

type Config struct {
	MaxPackBytes    int
	MaxEntries      int
	MaxFileBytes    int64
	MaxScanBytes    int64
	MaxScanFiles    int
	MaxSnippetBytes int
	MaxTerms        int
	CacheEntries    int
}

type Request struct {
	Root             string
	Contract         domain.GoalContract
	ExpectedRevision string
}

type Entry struct {
	Path      string   `json:"path"`
	Status    string   `json:"status,omitempty"`
	SHA256    string   `json:"sha256"`
	Score     int      `json:"score"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Reasons   []string `json:"reasons"`
	Text      string   `json:"text"`
	Truncated bool     `json:"truncated"`
}

type Pack struct {
	Revision     string   `json:"revision"`
	GoalHash     string   `json:"goal_hash"`
	Terms        []string `json:"terms"`
	Entries      []Entry  `json:"entries"`
	ScannedFiles int      `json:"scanned_files"`
	SkippedFiles int      `json:"skipped_files"`
	Bytes        int      `json:"bytes"`
	Truncated    bool     `json:"truncated"`
}

type Engine struct {
	repository Repository
	cfg        Config
	cache      *analysisCache
}

type weightedTerm struct {
	Term   string
	Weight int
}

type candidate struct {
	file       RepositoryFile
	source     []byte
	hash       string
	score      int
	bestLine   int
	reasons    map[string]struct{}
	goAnalysis goFileAnalysis
}

func New(repository Repository, cfg Config) (*Engine, error) {
	if repository == nil {
		return nil, errors.New("context repository source is required")
	}
	cfg = withDefaults(cfg)
	if cfg.MaxPackBytes < 512 || cfg.MaxEntries <= 0 || cfg.MaxFileBytes <= 0 || cfg.MaxScanBytes <= 0 || cfg.MaxScanFiles <= 0 || cfg.MaxSnippetBytes <= 0 || cfg.MaxTerms <= 0 || cfg.CacheEntries <= 0 {
		return nil, errors.New("context engine limits must be positive and max pack bytes must be at least 512")
	}
	if int64(cfg.MaxSnippetBytes) > int64(cfg.MaxPackBytes) {
		cfg.MaxSnippetBytes = cfg.MaxPackBytes / 2
	}
	return &Engine{repository: repository, cfg: cfg, cache: newAnalysisCache(cfg.CacheEntries)}, nil
}

func withDefaults(cfg Config) Config {
	if cfg.MaxPackBytes <= 0 {
		cfg.MaxPackBytes = 64 << 10
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 12
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 512 << 10
	}
	if cfg.MaxScanBytes <= 0 {
		cfg.MaxScanBytes = 8 << 20
	}
	if cfg.MaxScanFiles <= 0 {
		cfg.MaxScanFiles = 2000
	}
	if cfg.MaxSnippetBytes <= 0 {
		cfg.MaxSnippetBytes = 6 << 10
	}
	if cfg.MaxTerms <= 0 {
		cfg.MaxTerms = 32
	}
	if cfg.CacheEntries <= 0 {
		cfg.CacheEntries = 256
	}
	return cfg
}

func (e *Engine) Build(ctx context.Context, req Request) (Pack, error) {
	if err := req.Contract.Validate(); err != nil {
		return Pack{}, fmt.Errorf("invalid goal contract: %w", err)
	}
	if strings.TrimSpace(req.Root) == "" {
		return Pack{}, errors.New("context root is required")
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return Pack{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Pack{}, fmt.Errorf("resolve context root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		if err != nil {
			return Pack{}, err
		}
		return Pack{}, errors.New("context root is not a directory")
	}

	snapshot, err := e.repository.Snapshot(ctx, root)
	if err != nil {
		return Pack{}, err
	}
	snapshot.Revision = strings.TrimSpace(snapshot.Revision)
	if snapshot.Revision == "" {
		return Pack{}, errors.New("repository snapshot revision is required")
	}
	if expected := strings.TrimSpace(req.ExpectedRevision); expected != "" && !strings.EqualFold(expected, snapshot.Revision) {
		return Pack{}, fmt.Errorf("%w: expected=%s actual=%s", ErrRevisionMismatch, expected, snapshot.Revision)
	}
	goalHash, err := req.Contract.Hash()
	if err != nil {
		return Pack{}, err
	}
	terms := collectTerms(req.Contract, e.cfg.MaxTerms)
	modulePath := readModulePath(root, e.cfg.MaxFileBytes)

	files := normalizeSnapshotFiles(snapshot.Files)
	queue := make([]RepositoryFile, 0, len(files))
	for _, file := range files {
		if skipRepositoryPath(file.Path) {
			continue
		}
		queue = append(queue, file)
	}
	sort.Slice(queue, func(i, j int) bool {
		si := metadataScore(queue[i], terms)
		sj := metadataScore(queue[j], terms)
		if si != sj {
			return si > sj
		}
		return queue[i].Path < queue[j].Path
	})

	candidates := make(map[string]*candidate, len(queue))
	var scannedBytes int64
	scannedFiles := 0
	skippedFiles := 0
	scanTruncated := false
	for _, file := range queue {
		if err := ctx.Err(); err != nil {
			return Pack{}, err
		}
		if scannedFiles >= e.cfg.MaxScanFiles || scannedBytes >= e.cfg.MaxScanBytes {
			scanTruncated = true
			break
		}
		source, resolvedBytes, skip, err := readContextFile(root, file.Path, e.cfg.MaxFileBytes)
		if err != nil || skip {
			skippedFiles++
			continue
		}
		if scannedBytes+resolvedBytes > e.cfg.MaxScanBytes {
			scanTruncated = true
			break
		}
		scannedBytes += resolvedBytes
		scannedFiles++
		hashBytes := sha256.Sum256(source)
		hash := hex.EncodeToString(hashBytes[:])
		analysis := e.cache.getOrCompute(hash, func() goFileAnalysis {
			return analyzeGoFile(file.Path, source)
		})
		c := &candidate{
			file:       file,
			source:     source,
			hash:       hash,
			reasons:    make(map[string]struct{}),
			goAnalysis: analysis,
		}
		c.score += metadataScoreWithReasons(file, terms, c.reasons)
		lexicalScore, bestLine := scoreLexical(source, terms, c.reasons)
		c.score += lexicalScore
		if bestLine > 0 {
			c.bestLine = bestLine
		}
		symbolScore, symbolLine := scoreSymbols(analysis, terms, c.reasons)
		c.score += symbolScore
		if symbolLine > 0 && (c.bestLine == 0 || symbolScore >= lexicalScore) {
			c.bestLine = symbolLine
		}
		candidates[file.Path] = c
	}

	applyDependencyBoosts(candidates, modulePath)
	ranked := make([]*candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.score > 0 {
			ranked = append(ranked, c)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].file.Path < ranked[j].file.Path
	})

	pack := Pack{
		Revision:     snapshot.Revision,
		GoalHash:     goalHash,
		Terms:        termNames(terms),
		ScannedFiles: scannedFiles,
		SkippedFiles: skippedFiles,
		Truncated:    scanTruncated,
	}
	limit := min(e.cfg.MaxEntries, len(ranked))
	for i := 0; i < limit; i++ {
		c := ranked[i]
		start, end, text, truncated := extractSnippet(c.source, c.bestLine, e.cfg.MaxSnippetBytes)
		pack.Entries = append(pack.Entries, Entry{
			Path:      c.file.Path,
			Status:    c.file.Status,
			SHA256:    c.hash,
			Score:     c.score,
			StartLine: start,
			EndLine:   end,
			Reasons:   sortedReasons(c.reasons),
			Text:      text,
			Truncated: truncated,
		})
	}
	if len(ranked) > limit {
		pack.Truncated = true
	}
	fitPack(&pack, e.cfg.MaxPackBytes)
	return pack, nil
}

func (p Pack) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CONTEXT_PACK\nrevision: %s\ngoal_hash: %s\nterms: %s\nscanned_files: %d\nskipped_files: %d\ntruncated: %t\n", p.Revision, p.GoalHash, strings.Join(p.Terms, ","), p.ScannedFiles, p.SkippedFiles, p.Truncated)
	for _, entry := range p.Entries {
		fmt.Fprintf(&b, "\n--- %s score=%d status=%s sha256=%s lines=%d-%d truncated=%t\nreasons: %s\n", entry.Path, entry.Score, entry.Status, entry.SHA256, entry.StartLine, entry.EndLine, entry.Truncated, strings.Join(entry.Reasons, ","))
		b.WriteString(entry.Text)
		if !strings.HasSuffix(entry.Text, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func normalizeSnapshotFiles(files []RepositoryFile) []RepositoryFile {
	byPath := make(map[string]RepositoryFile, len(files))
	for _, file := range files {
		path, err := cleanRepositoryPath(file.Path)
		if err != nil {
			continue
		}
		file.Path = filepath.ToSlash(path)
		file.Status = strings.TrimSpace(strings.ToLower(file.Status))
		if existing, ok := byPath[file.Path]; ok {
			if existing.Status == "" || existing.Status == "clean" {
				byPath[file.Path] = file
			}
			continue
		}
		byPath[file.Path] = file
	}
	out := make([]RepositoryFile, 0, len(byPath))
	for _, file := range byPath {
		out = append(out, file)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func cleanRepositoryPath(path string) (string, error) {
	path = filepath.FromSlash(path)
	if path == "" || filepath.IsAbs(path) {
		return "", errors.New("repository path must be relative")
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return "", errors.New("repository path contains control characters")
		}
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("repository path escapes root")
	}
	return clean, nil
}

func skipRepositoryPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	for _, prefix := range []string{".git/", ".mar/", "node_modules/", "vendor/", ".venv/", "dist/", "build/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == ".git" || path == ".mar" || path == "node_modules" || path == "vendor" || path == ".venv" || path == "dist" || path == "build"
}

func readContextFile(root, rel string, maxBytes int64) ([]byte, int64, bool, error) {
	clean, err := cleanRepositoryPath(rel)
	if err != nil {
		return nil, 0, true, err
	}
	candidate := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, 0, true, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || !insideRoot(root, resolved) {
		return nil, 0, true, errors.New("context file resolves outside root")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, 0, true, err
	}
	if info.Size() > maxBytes {
		return nil, info.Size(), true, nil
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, 0, true, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, 0, true, err
	}
	if int64(len(data)) > maxBytes || looksBinary(data) {
		return nil, int64(len(data)), true, nil
	}
	return data, int64(len(data)), false, nil
}

func insideRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func looksBinary(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	limit := min(len(data), 8<<10)
	for _, b := range data[:limit] {
		if b == 0 {
			return true
		}
	}
	return false
}

const (
	maxContextIntentTextBytes = 64 << 10
	maxContextTermRunes       = 96
)

func collectTerms(contract domain.GoalContract, maxTerms int) []weightedTerm {
	weights := make(map[string]int)
	maxUnique := max(16, maxTerms*4)
	addWeightedTerms(weights, contract.Goal, 5, maxUnique)
	for _, value := range contract.Acceptance {
		addWeightedTerms(weights, value, 3, maxUnique)
	}
	for _, value := range contract.Boundaries {
		addWeightedTerms(weights, value, 1, maxUnique)
	}
	terms := make([]weightedTerm, 0, len(weights))
	for term, weight := range weights {
		terms = append(terms, weightedTerm{Term: term, Weight: weight})
	}
	sort.Slice(terms, func(i, j int) bool {
		if terms[i].Weight != terms[j].Weight {
			return terms[i].Weight > terms[j].Weight
		}
		return terms[i].Term < terms[j].Term
	})
	if len(terms) > maxTerms {
		terms = terms[:maxTerms]
	}
	return terms
}

var contextStopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "this": {}, "that": {}, "into": {}, "when": {}, "then": {}, "must": {}, "should": {}, "will": {}, "without": {},
	"và": {}, "của": {}, "cho": {}, "với": {}, "trong": {}, "một": {}, "các": {}, "được": {}, "là": {}, "không": {}, "phải": {},
}

func addWeightedTerms(dst map[string]int, text string, weight, maxUnique int) {
	for _, term := range tokenize(text) {
		if len([]rune(term)) < 3 {
			continue
		}
		if _, stop := contextStopWords[term]; stop {
			continue
		}
		if current, exists := dst[term]; exists {
			if weight > current {
				dst[term] = weight
			}
			continue
		}
		if len(dst) >= maxUnique {
			continue
		}
		dst[term] = weight
	}
}

func tokenize(text string) []string {
	text = truncateUTF8(text, maxContextIntentTextBytes)
	var terms []string
	var current []rune
	overflow := false
	flush := func() {
		if len(current) > 0 && !overflow {
			terms = append(terms, strings.ToLower(string(current)))
		}
		current = current[:0]
		overflow = false
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			if len(current) < maxContextTermRunes {
				current = append(current, unicode.ToLower(r))
			} else {
				overflow = true
			}
			continue
		}
		flush()
	}
	flush()
	return terms
}

func termNames(terms []weightedTerm) []string {
	out := make([]string, len(terms))
	for i, term := range terms {
		out[i] = term.Term
	}
	return out
}

func metadataScore(file RepositoryFile, terms []weightedTerm) int {
	return metadataScoreWithReasons(file, terms, nil)
}

func metadataScoreWithReasons(file RepositoryFile, terms []weightedTerm, reasons map[string]struct{}) int {
	score := 0
	lowerPath := strings.ToLower(filepath.ToSlash(file.Path))
	for _, term := range terms {
		if strings.Contains(lowerPath, term.Term) {
			score += 8 * term.Weight
			if reasons != nil {
				reasons["path:"+term.Term] = struct{}{}
			}
		}
	}
	if file.Status != "" && file.Status != "clean" {
		score += 6
		if reasons != nil {
			reasons["git:"+file.Status] = struct{}{}
		}
	}
	base := strings.ToLower(filepath.Base(file.Path))
	if base == "readme.md" || base == "agents.md" || base == "go.mod" || base == "package.json" || base == "pyproject.toml" {
		score += 2
		if reasons != nil {
			reasons["repo-metadata"] = struct{}{}
		}
	}
	return score
}

func scoreLexical(source []byte, terms []weightedTerm, reasons map[string]struct{}) (int, int) {
	lines := strings.Split(string(source), "\n")
	score := 0
	bestLine := 0
	bestWeight := -1
	for lineIndex, line := range lines {
		lower := strings.ToLower(line)
		for _, term := range terms {
			count := strings.Count(lower, term.Term)
			if count == 0 {
				continue
			}
			score += min(count, 3) * 3 * term.Weight
			reasons["lexical:"+term.Term] = struct{}{}
			if term.Weight > bestWeight {
				bestWeight = term.Weight
				bestLine = lineIndex + 1
			}
		}
	}
	return score, bestLine
}

func scoreSymbols(analysis goFileAnalysis, terms []weightedTerm, reasons map[string]struct{}) (int, int) {
	score := 0
	bestLine := 0
	bestWeight := -1
	for _, symbol := range analysis.Symbols {
		lower := strings.ToLower(symbol.Name)
		for _, term := range terms {
			if !strings.Contains(lower, term.Term) {
				continue
			}
			score += 12 * term.Weight
			reasons["symbol:"+symbol.Name] = struct{}{}
			if term.Weight > bestWeight {
				bestWeight = term.Weight
				bestLine = symbol.Line
			}
		}
	}
	return score, bestLine
}

func applyDependencyBoosts(candidates map[string]*candidate, modulePath string) {
	if modulePath == "" || len(candidates) == 0 {
		return
	}
	initial := make([]*candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.score > 0 && len(c.goAnalysis.Imports) > 0 {
			initial = append(initial, c)
		}
	}
	sort.Slice(initial, func(i, j int) bool {
		if initial[i].score != initial[j].score {
			return initial[i].score > initial[j].score
		}
		return initial[i].file.Path < initial[j].file.Path
	})
	if len(initial) > 5 {
		initial = initial[:5]
	}
	for _, source := range initial {
		for _, importPath := range source.goAnalysis.Imports {
			dir, ok := localImportDir(modulePath, importPath)
			if !ok {
				continue
			}
			for path, target := range candidates {
				if filepath.ToSlash(filepath.Dir(path)) != dir || !strings.HasSuffix(strings.ToLower(path), ".go") {
					continue
				}
				target.score += 9
				target.reasons["dependency:"+importPath] = struct{}{}
			}
		}
		packageDir := filepath.ToSlash(filepath.Dir(source.file.Path))
		for path, sibling := range candidates {
			if path == source.file.Path || filepath.ToSlash(filepath.Dir(path)) != packageDir || !strings.HasSuffix(strings.ToLower(path), ".go") {
				continue
			}
			sibling.score += 3
			sibling.reasons["dependency:same-package"] = struct{}{}
		}
	}
}

func localImportDir(modulePath, importPath string) (string, bool) {
	modulePath = strings.TrimSuffix(strings.TrimSpace(modulePath), "/")
	importPath = strings.TrimSpace(importPath)
	if modulePath == "" || importPath == "" {
		return "", false
	}
	if importPath == modulePath {
		return ".", true
	}
	prefix := modulePath + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return filepath.ToSlash(strings.TrimPrefix(importPath, prefix)), true
}

func readModulePath(root string, maxBytes int64) string {
	data, _, skip, err := readContextFile(root, "go.mod", maxBytes)
	if err != nil || skip {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func extractSnippet(source []byte, bestLine, maxBytes int) (int, int, string, bool) {
	lines := strings.Split(string(source), "\n")
	if len(lines) == 0 {
		return 1, 1, "", false
	}
	if bestLine <= 0 || bestLine > len(lines) {
		bestLine = 1
	}
	start := max(1, bestLine-18)
	end := min(len(lines), bestLine+22)
	text := strings.Join(lines[start-1:end], "\n")
	if end < len(lines) {
		text += "\n"
	}
	truncated := false
	if len(text) > maxBytes {
		text = truncateUTF8(text, maxBytes)
		truncated = true
	}
	return start, end, text, truncated
}

func sortedReasons(reasons map[string]struct{}) []string {
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func fitPack(pack *Pack, maxBytes int) {
	if pack == nil {
		return
	}
	for {
		rendered := pack.Render()
		if len(rendered) <= maxBytes {
			pack.Bytes = len(rendered)
			return
		}
		pack.Truncated = true
		if len(pack.Terms) > 0 {
			pack.Terms = pack.Terms[:len(pack.Terms)-1]
			continue
		}
		if len(pack.Entries) == 0 {
			pack.Bytes = len(rendered)
			return
		}
		last := &pack.Entries[len(pack.Entries)-1]
		over := len(rendered) - maxBytes
		if len(last.Text) > over+64 {
			last.Text = truncateUTF8(last.Text, len(last.Text)-over-32)
			last.Truncated = true
			continue
		}
		pack.Entries = pack.Entries[:len(pack.Entries)-1]
	}
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
