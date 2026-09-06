package contextengine

import (
	"math"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	rrfRankConstant = 60.0
	pageRankDamping = 0.85
	pageRankRounds  = 20
)

type retrievalTermStats struct {
	Path   int
	Symbol int
	Body   int
}

type retrievalStats struct {
	Terms       map[string]retrievalTermStats
	PathLen     int
	SymbolLen   int
	BodyLen     int
	BM25        float64
	SymbolExact int
	PathExact   int
	Changed     int
	PageRank    float64
}

// rankCandidates implements the finite MAR V1 retrieval policy:
// BM25F-style text relevance + exact symbol/path ranks + local dependency
// Personalized PageRank, fused with RRF. It is intentionally model-free and
// deterministic so the context pack remains revision-bound evidence.
func rankCandidates(candidates map[string]*candidate, terms []weightedTerm, modulePath string) {
	if len(candidates) == 0 {
		return
	}
	query := make(map[string]struct{}, len(terms))
	termWeight := make(map[string]int, len(terms))
	for _, term := range terms {
		query[term.Term] = struct{}{}
		termWeight[term.Term] = term.Weight
	}

	stats := make(map[string]*retrievalStats, len(candidates))
	docFreq := make(map[string]int, len(terms))
	var totalPathLen, totalSymbolLen, totalBodyLen int
	for path, c := range candidates {
		s := &retrievalStats{Terms: make(map[string]retrievalTermStats, len(terms))}
		pathCounts, pathLen := scanQueryTerms(path, query)
		s.PathLen = pathLen
		totalPathLen += pathLen

		var symbolText strings.Builder
		for _, symbol := range c.goAnalysis.Symbols {
			symbolText.WriteString(symbol.Name)
			symbolText.WriteByte(' ')
			forms := identifierForms(symbol.Name)
			for _, term := range terms {
				for _, form := range forms {
					if form == term.Term {
						s.SymbolExact += 10 * term.Weight
						c.reasons["symbol-exact:"+symbol.Name] = struct{}{}
						break
					}
				}
			}
		}
		symbolCounts, symbolLen := scanQueryTerms(symbolText.String(), query)
		s.SymbolLen = symbolLen
		totalSymbolLen += symbolLen

		bodyCounts, bodyLen := scanQueryTerms(string(c.source), query)
		s.BodyLen = bodyLen
		totalBodyLen += bodyLen
		pathForms := fieldForms(path)
		for _, term := range terms {
			if _, ok := pathForms[term.Term]; ok {
				s.PathExact += 4 * term.Weight
				c.reasons["path-exact:"+term.Term] = struct{}{}
			}
			value := retrievalTermStats{Path: pathCounts[term.Term], Symbol: symbolCounts[term.Term], Body: bodyCounts[term.Term]}
			s.Terms[term.Term] = value
			if value.Path+value.Symbol+value.Body > 0 {
				docFreq[term.Term]++
			}
		}
		if c.file.Status != "" && c.file.Status != "clean" {
			s.Changed = 1
		}
		stats[path] = s
	}

	n := float64(len(candidates))
	avgPath := maxFloat(1, float64(totalPathLen)/n)
	avgSymbol := maxFloat(1, float64(totalSymbolLen)/n)
	avgBody := maxFloat(1, float64(totalBodyLen)/n)
	for path, s := range stats {
		for _, term := range terms {
			tf := s.Terms[term.Term]
			weightedTF := 4*bm25FieldTF(tf.Path, s.PathLen, avgPath, 0.2) +
				6*bm25FieldTF(tf.Symbol, s.SymbolLen, avgSymbol, 0.3) +
				bm25FieldTF(tf.Body, s.BodyLen, avgBody, 0.75)
			if weightedTF <= 0 {
				continue
			}
			df := float64(docFreq[term.Term])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			const k1 = 1.2
			s.BM25 += float64(term.Weight) * idf * ((k1 + 1) * weightedTF / (k1 + weightedTF))
		}
		_ = path
	}

	edges := buildDependencyGraph(candidates, modulePath)
	personalization := make(map[string]float64)
	for path, s := range stats {
		seed := s.BM25 + float64(s.SymbolExact+s.PathExact)
		if seed > 0 {
			personalization[path] = seed
		}
	}
	if len(personalization) == 0 {
		for path, s := range stats {
			if s.Changed > 0 {
				personalization[path] = 1
			}
		}
	}
	pageRank := personalizedPageRank(candidates, edges, personalization)
	for path, score := range pageRank {
		stats[path].PageRank = score
		if score > 0 && len(edges) > 0 {
			candidates[path].reasons["structural:pagerank"] = struct{}{}
		}
	}

	ranks := [][]string{
		rankFloat(stats, func(s *retrievalStats) float64 { return s.BM25 }),
		rankInt(stats, func(s *retrievalStats) int { return s.SymbolExact }),
		rankInt(stats, func(s *retrievalStats) int { return s.PathExact }),
		rankFloat(stats, func(s *retrievalStats) float64 { return s.PageRank }),
		rankInt(stats, func(s *retrievalStats) int { return s.Changed }),
	}
	fused := make(map[string]float64, len(candidates))
	for _, ranking := range ranks {
		for i, path := range ranking {
			fused[path] += 1 / (rrfRankConstant + float64(i+1))
		}
	}
	for path, c := range candidates {
		c.score = int(math.Round(fused[path] * 1_000_000))
		if c.score > 0 {
			c.reasons["rank:rrf"] = struct{}{}
		}
	}
}

func bm25FieldTF(tf, length int, avgLength, b float64) float64 {
	if tf <= 0 {
		return 0
	}
	denom := 1 - b + b*float64(max(1, length))/avgLength
	return float64(tf) / denom
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func rankFloat(stats map[string]*retrievalStats, value func(*retrievalStats) float64) []string {
	paths := make([]string, 0, len(stats))
	for path, s := range stats {
		if value(s) > 1e-15 {
			paths = append(paths, path)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := value(stats[paths[i]]), value(stats[paths[j]])
		if math.Abs(a-b) > 1e-15 {
			return a > b
		}
		return paths[i] < paths[j]
	})
	return paths
}

func rankInt(stats map[string]*retrievalStats, value func(*retrievalStats) int) []string {
	paths := make([]string, 0, len(stats))
	for path, s := range stats {
		if value(s) > 0 {
			paths = append(paths, path)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := value(stats[paths[i]]), value(stats[paths[j]])
		if a != b {
			return a > b
		}
		return paths[i] < paths[j]
	})
	return paths
}

func scanQueryTerms(text string, query map[string]struct{}) (map[string]int, int) {
	counts := make(map[string]int, len(query))
	length := 0
	var token []rune
	flush := func() {
		if len(token) == 0 {
			return
		}
		length++
		for _, form := range identifierForms(string(token)) {
			if _, ok := query[form]; ok {
				counts[form]++
			}
		}
		token = token[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' {
			if len(token) < maxContextTermRunes {
				token = append(token, r)
			}
			continue
		}
		flush()
	}
	flush()
	return counts, length
}

func fieldForms(text string) map[string]struct{} {
	forms := make(map[string]struct{})
	var token []rune
	flush := func() {
		if len(token) == 0 {
			return
		}
		for _, form := range identifierForms(string(token)) {
			forms[form] = struct{}{}
		}
		token = token[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' {
			token = append(token, r)
			continue
		}
		flush()
	}
	flush()
	return forms
}

func identifierForms(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	forms := []string{strings.ToLower(raw)}
	var part []rune
	runes := []rune(raw)
	flush := func() {
		if len(part) == 0 {
			return
		}
		value := strings.ToLower(string(part))
		if value != "" && value != forms[0] {
			forms = append(forms, value)
		}
		part = part[:0]
	}
	for i, r := range runes {
		if r == '_' || r == '-' || r == '$' {
			flush()
			continue
		}
		if len(part) > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		part = append(part, r)
	}
	flush()
	seen := make(map[string]struct{}, len(forms))
	out := forms[:0]
	for _, form := range forms {
		if form == "" {
			continue
		}
		if _, ok := seen[form]; ok {
			continue
		}
		seen[form] = struct{}{}
		out = append(out, form)
	}
	return out
}

func buildDependencyGraph(candidates map[string]*candidate, modulePath string) map[string]map[string]float64 {
	edges := make(map[string]map[string]float64)
	for sourcePath, c := range candidates {
		for _, dependency := range c.goAnalysis.Imports {
			for _, target := range resolveDependencyTargets(sourcePath, c.goAnalysis.Language, dependency, candidates, modulePath) {
				if target == sourcePath {
					continue
				}
				if edges[sourcePath] == nil {
					edges[sourcePath] = make(map[string]float64)
				}
				edges[sourcePath][target]++
				candidates[target].reasons["dependency:"+dependency] = struct{}{}
			}
		}
	}
	return edges
}

func resolveDependencyTargets(sourcePath, language, dependency string, candidates map[string]*candidate, modulePath string) []string {
	switch language {
	case "go":
		dir, ok := localImportDir(modulePath, dependency)
		if !ok {
			return nil
		}
		var out []string
		for path := range candidates {
			if filepath.ToSlash(filepath.Dir(path)) == dir && strings.EqualFold(filepath.Ext(path), ".go") {
				out = append(out, path)
			}
		}
		sort.Strings(out)
		return out
	case "python":
		return existingGraphTargets(pythonDependencyCandidates(sourcePath, dependency), candidates)
	case "javascript":
		return existingGraphTargets(javaScriptDependencyCandidates(sourcePath, dependency), candidates)
	default:
		return nil
	}
}

func pythonDependencyCandidates(sourcePath, dependency string) []string {
	dependency = strings.TrimSpace(dependency)
	if dependency == "" {
		return nil
	}
	base := "."
	module := dependency
	if strings.HasPrefix(dependency, ".") {
		dots := 0
		for dots < len(dependency) && dependency[dots] == '.' {
			dots++
		}
		base = pathpkg.Dir(filepath.ToSlash(sourcePath))
		for i := 1; i < dots; i++ {
			base = pathpkg.Dir(base)
		}
		module = strings.TrimPrefix(dependency, strings.Repeat(".", dots))
	}
	modulePath := strings.ReplaceAll(module, ".", "/")
	stem := pathpkg.Clean(pathpkg.Join(base, modulePath))
	if stem == "." || stem == ".." || strings.HasPrefix(stem, "../") {
		return nil
	}
	return []string{stem + ".py", pathpkg.Join(stem, "__init__.py")}
}

func javaScriptDependencyCandidates(sourcePath, dependency string) []string {
	dependency = strings.TrimSpace(dependency)
	if !strings.HasPrefix(dependency, "./") && !strings.HasPrefix(dependency, "../") {
		return nil
	}
	stem := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(filepath.ToSlash(sourcePath)), dependency))
	if stem == ".." || strings.HasPrefix(stem, "../") {
		return nil
	}
	if ext := pathpkg.Ext(stem); ext != "" {
		return []string{stem}
	}
	extensions := []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs", ".json"}
	out := make([]string, 0, len(extensions)*2)
	for _, ext := range extensions {
		out = append(out, stem+ext, pathpkg.Join(stem, "index"+ext))
	}
	return out
}

func existingGraphTargets(paths []string, candidates map[string]*candidate) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		path = filepath.ToSlash(pathpkg.Clean(path))
		if _, ok := candidates[path]; !ok {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func personalizedPageRank(candidates map[string]*candidate, edges map[string]map[string]float64, seeds map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(candidates))
	if len(candidates) == 0 || len(seeds) == 0 {
		return result
	}
	var seedTotal float64
	for path, weight := range seeds {
		if _, ok := candidates[path]; ok && weight > 0 {
			seedTotal += weight
		}
	}
	if seedTotal <= 0 {
		return result
	}
	personalization := make(map[string]float64, len(candidates))
	for path, weight := range seeds {
		if _, ok := candidates[path]; ok && weight > 0 {
			personalization[path] = weight / seedTotal
		}
	}
	for path := range candidates {
		result[path] = personalization[path]
	}
	for iteration := 0; iteration < pageRankRounds; iteration++ {
		next := make(map[string]float64, len(candidates))
		for path := range candidates {
			next[path] = (1 - pageRankDamping) * personalization[path]
		}
		for source, rank := range result {
			outgoing := edges[source]
			var total float64
			for target, weight := range outgoing {
				if _, ok := candidates[target]; ok && weight > 0 {
					total += weight
				}
			}
			if total <= 0 {
				for target, weight := range personalization {
					next[target] += pageRankDamping * rank * weight
				}
				continue
			}
			for target, weight := range outgoing {
				if _, ok := candidates[target]; ok && weight > 0 {
					next[target] += pageRankDamping * rank * weight / total
				}
			}
		}
		result = next
	}
	return result
}
