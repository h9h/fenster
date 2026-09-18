package layout

import (
	"path/filepath"
	"strings"

	"fenster/internal/store"
)

// SimilarityThreshold is the lowest normalized Levenshtein similarity at which
// two titles of the same executable are still considered the same window. Tune
// here if real titles turn out to drift more or less than expected.
const SimilarityThreshold = 0.6

// Match pairs a saved entry with the live window it will be applied to.
type Match struct {
	Index int // index of the entry within Layout.Windows
	Entry store.WindowEntry
	Live  Live
}

// Plan is the outcome of matching: what can be restored and what is missing.
type Plan struct {
	Matches []Match
	Missing []store.WindowEntry
}

// MatchEntries pairs saved entries with currently open windows. Three passes
// run in order over everything still unmatched: exact title, similar title,
// then ordinal. A live window is consumed by at most one entry, so duplicate
// windows of the same application are distributed instead of stacked.
func MatchEntries(entries []store.WindowEntry, live []Live) Plan {
	candidates := make([]Live, 0, len(live))
	for _, w := range live {
		if Eligible(w) {
			candidates = append(candidates, w)
		}
	}

	taken := make([]bool, len(candidates))
	matched := make([]*Match, len(entries))

	pass := func(pick func(e store.WindowEntry) int) {
		for i, e := range entries {
			if matched[i] != nil {
				continue
			}
			if j := pick(e); j >= 0 {
				taken[j] = true
				matched[i] = &Match{Index: i, Entry: e, Live: candidates[j]}
			}
		}
	}

	sameExe := func(e store.WindowEntry, j int) bool {
		return !taken[j] && strings.EqualFold(candidates[j].Exe, e.Exe)
	}

	// Pass 1: same executable, exact title.
	pass(func(e store.WindowEntry) int {
		for j := range candidates {
			if sameExe(e, j) && candidates[j].Title == e.Title {
				return j
			}
		}
		return -1
	})

	// Pass 2: same executable, best similar title above the threshold. Ties
	// go to the first candidate in enumeration order (strict > below), so the
	// result is deterministic.
	pass(func(e store.WindowEntry) int {
		best, bestScore := -1, -1.0
		want := NormalizeTitle(e.Title, e.Exe)
		for j := range candidates {
			if !sameExe(e, j) {
				continue
			}
			score := Similarity(want, NormalizeTitle(candidates[j].Title, candidates[j].Exe))
			if score >= SimilarityThreshold && score > bestScore {
				best, bestScore = j, score
			}
		}
		return best
	})

	// Pass 3: same executable, the first still-free window in enumeration
	// order. Entries are processed in save order, so entry ordinal 0 claims
	// the first free window, ordinal 1 the next, and so on.
	pass(func(e store.WindowEntry) int {
		for j := range candidates {
			if sameExe(e, j) {
				return j
			}
		}
		return -1
	})

	var plan Plan
	for i, m := range matched {
		if m == nil {
			plan.Missing = append(plan.Missing, entries[i])
			continue
		}
		plan.Matches = append(plan.Matches, *m)
	}
	return plan
}

// titleTrimCutset are the modification markers applications prepend to titles.
const titleTrimCutset = " \t●•*◐○·—-"

// NormalizeTitle strips modification markers and case, so that everyday title
// churn does not break matching. It also strips a trailing " - AppName"
// suffix, but only when that suffix actually names exe (its base name without
// extension, compared case-insensitively); an unrelated trailing segment,
// such as a genuine part of the window's own title, is left in place.
func NormalizeTitle(title, exe string) string {
	s := strings.TrimLeft(title, titleTrimCutset)
	appName := appBaseName(exe)
	for _, sep := range []string{" — ", " – ", " - "} {
		i := strings.LastIndex(s, sep)
		if i <= 0 {
			continue
		}
		suffix := strings.ToLower(strings.TrimSpace(s[i+len(sep):]))
		if suffix == appName {
			s = s[:i]
		}
		break
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// appBaseName returns the lower-cased file name of exe without its
// extension, e.g. `C:\a\Editor.exe` -> "editor".
func appBaseName(exe string) string {
	base := filepath.Base(exe)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.ToLower(base)
}

// Similarity returns 1 for identical strings and 0 for entirely different
// ones, as 1 - levenshtein/maxLen.
func Similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	dist := levenshtein(ra, rb)
	longest := len(ra)
	if len(rb) > longest {
		longest = len(rb)
	}
	return 1 - float64(dist)/float64(longest)
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
