package agent

// Pre-compression golden harness for the token-optimization work.
//
// Two complementary guards over the personas in the three eval configs
// (judge_team.json, skwad_review_team.json, classifier_team.json):
//
//  1. TestPersonaSizes_Ratchet — records per-persona size (persona char/rune
//     count + rendered BuildSystemPrompt length) as a golden baseline and
//     asserts sizes never GROW past it. Proves compression shrinks the
//     personas and catches accidental growth. Re-baseline with -updategolden
//     after a legitimate compression to ratchet the budget down.
//
//  2. TestPersonaCriteria_Tripwire — extracts the LOAD-BEARING, enumerated
//     scoring criteria from each persona (severity scale, judge scoring
//     dimensions + point ranges + verification buckets, classifier difficulty
//     buckets) and asserts the SET is byte-for-byte unchanged. This is a strict
//     tripwire: dropping/renaming/merging a severity tier or a scoring
//     dimension fails the build.
//
// What the tripwire deliberately does NOT guard (see the report to the
// Manager): prose responsibilities, "How to Review" steps, and Rule bullets.
// Legitimate compression (dedup, filler removal) changes those counts, so any
// assertion there would false-fail on allowed edits. The Reviewer's
// item-by-item human diff remains the PRIMARY fidelity gate for prose.

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// updateGolden regenerates the committed golden snapshots instead of asserting
// against them. Run: go test ./internal/agent/ -run Persona -updategolden
var updateGolden = flag.Bool("updategolden", false, "regenerate persona golden snapshots")

// evalConfigs lists the three configs the eval harness loads, relative to this
// package directory (internal/agent → repo root is ../../).
var evalConfigs = []string{
	"../../eval/config/judge_team.json",
	"../../test_configs/skwad_review_team.json",
	"../../eval/config/classifier_team.json",
}

const (
	sizesGoldenPath    = "testdata/persona_sizes.golden.json"
	criteriaGoldenPath = "testdata/persona_criteria.golden.json"
)

// personaEntry is one persona resolved from a config, keyed for stable
// snapshotting. Key is "<config-basename>::<persona-name>" so personas are
// unambiguous across configs.
type personaEntry struct {
	key          string
	configFile   string
	agentName    string
	personaName  string
	instructions string
}

// loadPersonaEntries parses every eval config (WITHOUT TeamConfig.Validate,
// which requires the repo path to exist on disk) and returns one entry per
// persona, sorted by key for determinism.
func loadPersonaEntries(t *testing.T) []personaEntry {
	t.Helper()
	var entries []personaEntry
	for _, path := range evalConfigs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read config %s: %v", path, err)
		}
		var tc config.TeamConfig
		if err := json.Unmarshal(data, &tc); err != nil {
			t.Fatalf("parse config %s: %v", path, err)
		}
		base := filepath.Base(path)
		// agentName per persona: match by AgentConfig.Persona == PersonaConfig.Name.
		agentByPersona := make(map[string]string)
		for _, a := range tc.Agents {
			if a.Persona != "" {
				agentByPersona[a.Persona] = a.Name
			}
		}
		for _, p := range tc.Personas {
			entries = append(entries, personaEntry{
				key:          base + "::" + p.Name,
				configFile:   base,
				agentName:    agentByPersona[p.Name],
				personaName:  p.Name,
				instructions: p.Instructions,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	if len(entries) == 0 {
		t.Fatal("no personas loaded from eval configs")
	}
	return entries
}

// ---------------------------------------------------------------------------
// Test 1: size ratchet
// ---------------------------------------------------------------------------

type personaSize struct {
	PersonaChars        int `json:"persona_chars"`
	PersonaRunes        int `json:"persona_runes"`
	RenderedPromptChars int `json:"rendered_prompt_chars"`
}

// renderedPromptLen renders the full BuildSystemPrompt for a persona with
// deterministic inputs (nil UUID, no teammates) so the length is reproducible.
// Fixed overhead layers (preamble, any matched role) are constant, so this
// length tracks the persona contribution.
func renderedPromptLen(e personaEntry) int {
	agent := &models.Agent{ID: uuid.Nil, Name: e.agentName}
	persona := &models.Persona{Name: e.personaName, Instructions: e.instructions}
	return len([]rune(BuildSystemPrompt(agent, persona, nil)))
}

func computeSizes(entries []personaEntry) map[string]personaSize {
	sizes := make(map[string]personaSize, len(entries))
	for _, e := range entries {
		sizes[e.key] = personaSize{
			PersonaChars:        len(e.instructions),
			PersonaRunes:        len([]rune(e.instructions)),
			RenderedPromptChars: renderedPromptLen(e),
		}
	}
	return sizes
}

func TestPersonaSizes_Ratchet(t *testing.T) {
	entries := loadPersonaEntries(t)
	current := computeSizes(entries)

	if *updateGolden {
		writeJSONGolden(t, sizesGoldenPath, current)
		t.Logf("wrote %d persona sizes to %s", len(current), sizesGoldenPath)
		return
	}

	var golden map[string]personaSize
	readJSONGolden(t, sizesGoldenPath, &golden)

	for _, e := range entries {
		g, ok := golden[e.key]
		if !ok {
			t.Errorf("persona %q missing from size golden — run with -updategolden", e.key)
			continue
		}
		c := current[e.key]
		// Ratchet: sizes may shrink (compression) but must never grow.
		if c.PersonaChars > g.PersonaChars {
			t.Errorf("%s: persona GREW %d → %d chars (budget exceeded)", e.key, g.PersonaChars, c.PersonaChars)
		}
		if c.RenderedPromptChars > g.RenderedPromptChars {
			t.Errorf("%s: rendered prompt GREW %d → %d chars", e.key, g.RenderedPromptChars, c.RenderedPromptChars)
		}
		if c.PersonaChars < g.PersonaChars {
			t.Logf("%s: persona shrank %d → %d chars (-%d)", e.key, g.PersonaChars, c.PersonaChars, g.PersonaChars-c.PersonaChars)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: criteria-preservation tripwire
// ---------------------------------------------------------------------------

// criteriaSig is the set of load-bearing, enumerated criteria extracted from a
// persona. Every field is a sorted, de-duplicated set so equality is order- and
// duplicate-insensitive.
type criteriaSig struct {
	SeverityLevels      []string `json:"severity_levels"`      // CRITICAL/HIGH/MEDIUM/LOW/NIT scale
	ScoringDimensions   []string `json:"scoring_dimensions"`   // judge: issue_detection, depth, ...
	ScoreRanges         []string `json:"score_ranges"`         // judge: "0-3 each", "21-point max"
	VerificationBuckets []string `json:"verification_buckets"` // judge: verified/unverified/...
	DifficultyBuckets   []string `json:"difficulty_buckets"`   // classifier: easy/medium/hard
}

var (
	// Severity tier as a list item in a severity scale: "- CRITICAL — ...".
	// Matches ANY all-caps tier token (>=2 letters) immediately followed by an
	// em-dash, not just the known vocabulary — so a NEWLY INVENTED tier name
	// (e.g. "- BLOCKER — ...") is also captured and a tier ADD trips the guard.
	// The em-dash anchor keeps it from matching prose like "- ONLY report ...".
	reSeverity = regexp.MustCompile(`(?m)^\s*[-*]\s+([A-Z]{2,})\s*—`)
	// Judge scoring dimension anchor: "issue_detection — count of ...".
	reDimension = regexp.MustCompile(`(?m)^([a-z][a-z_]{2,}) — `)
	// Score ranges in the judge rubric.
	rePointMax  = regexp.MustCompile(`(\d+)-point max`)
	rePerCrit   = regexp.MustCompile(`\(0-(\d+) each`)
	reBucketEnum = regexp.MustCompile(`"(easy|medium|hard)"`)
)

func extractCriteria(s string) criteriaSig {
	sig := criteriaSig{
		SeverityLevels:    uniqueSorted(captureGroup(reSeverity, s, 1)),
		ScoringDimensions: uniqueSorted(captureGroup(reDimension, s, 1)),
		DifficultyBuckets: uniqueSorted(captureGroup(reBucketEnum, s, 1)),
	}

	var ranges []string
	if m := rePointMax.FindStringSubmatch(s); m != nil {
		ranges = append(ranges, m[1]+"-point max")
	}
	if m := rePerCrit.FindStringSubmatch(s); m != nil {
		ranges = append(ranges, "0-"+m[1]+" each")
	}
	sig.ScoreRanges = uniqueSorted(ranges)

	// Verification buckets are load-bearing literals in the judge rubric.
	var buckets []string
	for _, b := range []string{"verified", "unverified", "contradicted", "non_falsifiable"} {
		if regexp.MustCompile(`\b` + b + `\b`).MatchString(s) {
			buckets = append(buckets, b)
		}
	}
	sig.VerificationBuckets = uniqueSorted(buckets)

	return sig
}

func captureGroup(re *regexp.Regexp, s string, group int) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[group])
	}
	return out
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]bool, len(in))
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func computeCriteria(entries []personaEntry) map[string]criteriaSig {
	out := make(map[string]criteriaSig, len(entries))
	for _, e := range entries {
		out[e.key] = extractCriteria(e.instructions)
	}
	return out
}

func TestPersonaCriteria_Tripwire(t *testing.T) {
	entries := loadPersonaEntries(t)
	current := computeCriteria(entries)

	if *updateGolden {
		writeJSONGolden(t, criteriaGoldenPath, current)
		t.Logf("wrote criteria for %d personas to %s", len(current), criteriaGoldenPath)
		return
	}

	var golden map[string]criteriaSig
	readJSONGolden(t, criteriaGoldenPath, &golden)

	for _, d := range diffCriteria(golden, current) {
		t.Error(d)
	}
}

// diffCriteria compares two persona→criteria maps and returns a sorted list of
// human-readable differences. An empty result means the maps are identical in
// BOTH membership (persona set: count + identity) AND per-persona criteria
// (severity tiers, scoring dimensions, ranges, buckets). It catches:
//   - a persona ADDED (key in current, not golden)
//   - a persona REMOVED (key in golden, not current)
//   - a persona RENAMED (surfaces as one add + one remove)
//   - any change to a persona's criteria SET (e.g. a severity tier added/dropped/renamed)
func diffCriteria(golden, current map[string]criteriaSig) []string {
	var diffs []string
	for key, c := range current {
		g, ok := golden[key]
		if !ok {
			diffs = append(diffs, "persona ADDED (not in golden): "+key+" — run -updategolden if intended")
			continue
		}
		if !reflect.DeepEqual(c, g) {
			diffs = append(diffs, fmt.Sprintf("criteria CHANGED for %q (load-bearing criterion added/dropped/renamed):\n  golden:  %+v\n  current: %+v", key, g, c))
		}
	}
	for key := range golden {
		if _, ok := current[key]; !ok {
			diffs = append(diffs, "persona REMOVED (in golden, not current): "+key+" — run -updategolden if intended")
		}
	}
	sort.Strings(diffs)
	return diffs
}

// ---------------------------------------------------------------------------
// Test 3: inline persona_instructions must stay in sync with the personas[] copy
// ---------------------------------------------------------------------------

// Each eval config duplicates the persona text in two places: the agents[]
// entry's persona_instructions and the personas[] entry's instructions. A
// compression that edits one but not the other is a bug — this catches it.
func TestPersonaInstructions_InSync(t *testing.T) {
	for _, path := range evalConfigs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read config %s: %v", path, err)
		}
		var tc config.TeamConfig
		if err := json.Unmarshal(data, &tc); err != nil {
			t.Fatalf("parse config %s: %v", path, err)
		}
		byName := make(map[string]string)
		for _, p := range tc.Personas {
			byName[p.Name] = p.Instructions
		}
		base := filepath.Base(path)
		for _, a := range tc.Agents {
			if a.PersonaInstructions == "" || a.Persona == "" {
				continue
			}
			want, ok := byName[a.Persona]
			if !ok {
				t.Errorf("%s: agent %q references persona %q with no personas[] entry", base, a.Name, a.Persona)
				continue
			}
			if a.PersonaInstructions != want {
				t.Errorf("%s: agent %q persona_instructions out of sync with personas[%q].instructions (lengths %d vs %d)",
					base, a.Name, a.Persona, len(a.PersonaInstructions), len(want))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Review Coordinator delegation mandate guard
// ---------------------------------------------------------------------------

// The Review Coordinator must orchestrate the specialists and NEVER review solo.
// That behavior lives only in persona prose — the criteria tripwire above does
// not cover it — so a future persona compression or edit could silently strip it
// (the exact failure that made the coordinator review solo before this change).
// This guard renders the FULL assembled system prompt (the real artifact the
// agent receives) and asserts it still mandates: dispatch via send-message,
// wait/collect via check-messages, and never review solo. Assertions are on
// durable key phrases, not exact whole-string matches, so legitimate rewording
// of the surrounding prose survives.
func TestReviewCoordinator_DelegationMandate(t *testing.T) {
	const coordKey = "skwad_review_team.json::Review Coordinator"

	entries := loadPersonaEntries(t)
	var coord *personaEntry
	for i := range entries {
		if entries[i].key == coordKey {
			coord = &entries[i]
			break
		}
	}
	if coord == nil {
		t.Fatalf("%q not found among loaded personas — config renamed or removed?", coordKey)
	}

	agent := &models.Agent{ID: uuid.Nil, Name: coord.agentName}
	persona := &models.Persona{Name: coord.personaName, Instructions: coord.instructions}
	prompt := BuildSystemPrompt(agent, persona, nil)

	// Durable substrings that encode the delegation mandate. If any goes
	// missing, a compression/edit dropped a load-bearing instruction.
	mustContain := []struct {
		phrase string
		why    string
	}{
		{"send-message", "must dispatch a task to each specialist via the send-message MCP tool"},
		{"check-messages", "must wait for and collect specialist findings via the check-messages MCP tool"},
		{"NEVER perform the code review yourself", "must never review solo — it is an orchestrator, not a reviewer"},
	}
	for _, mc := range mustContain {
		if !strings.Contains(prompt, mc.phrase) {
			t.Errorf("Review Coordinator system prompt missing delegation mandate: %q absent (%s)", mc.phrase, mc.why)
		}
	}
}

// ---------------------------------------------------------------------------
// Tripwire self-test: prove the extractor actually fires on a dropped/altered
// criterion. Guards against the tripwire silently becoming theater.
// ---------------------------------------------------------------------------

func TestExtractCriteria_DetectsDrops(t *testing.T) {
	const severityScale = "### Severity Scale\n\n" +
		"- CRITICAL — outage\n- HIGH — bad\n- MEDIUM — meh\n- LOW — minor\n- NIT — style\n"
	const judgeRubric = "CRITERIA (0-3 each, 21-point max):\n\n" +
		"issue_detection — count of issues\nactionability — how actionable\ndepth — reasoning\n" +
		"buckets: verified / unverified / contradicted / non_falsifiable\n"

	cases := []struct {
		name     string
		original string
		mutated  string
	}{
		{
			name:     "dropped severity tier",
			original: severityScale,
			mutated:  "### Severity Scale\n\n- CRITICAL — outage\n- HIGH — bad\n- MEDIUM — meh\n- LOW — minor\n", // NIT removed
		},
		{
			name:     "renamed severity tier",
			original: severityScale,
			mutated:  "### Severity Scale\n\n- CRITICAL — outage\n- HIGH — bad\n- MEDIUM — meh\n- LOW — minor\n- BLOCKER — style\n",
		},
		{
			name:     "added severity tier",
			original: severityScale,
			mutated:  severityScale + "- BLOCKER — new tier\n",
		},
		{
			name:     "dropped scoring dimension",
			original: judgeRubric,
			mutated:  "CRITERIA (0-3 each, 21-point max):\n\nissue_detection — count\nactionability — how\nbuckets: verified / unverified / contradicted / non_falsifiable\n", // depth removed
		},
		{
			name:     "changed point max",
			original: judgeRubric,
			mutated:  "CRITERIA (0-3 each, 18-point max):\n\nissue_detection — count\nactionability — how\ndepth — reasoning\nbuckets: verified / unverified / contradicted / non_falsifiable\n",
		},
		{
			name:     "dropped verification bucket",
			original: judgeRubric,
			mutated:  "CRITERIA (0-3 each, 21-point max):\n\nissue_detection — count\nactionability — how\ndepth — reasoning\nbuckets: verified / unverified / contradicted\n", // non_falsifiable removed
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := extractCriteria(tc.original)
			after := extractCriteria(tc.mutated)
			if reflect.DeepEqual(before, after) {
				t.Errorf("tripwire FAILED to detect %q — extractor is theater\n  before: %+v\n  after:  %+v", tc.name, before, after)
			}
		})
	}

	// Control: pure rephrasing that preserves all criteria must NOT trip.
	reworded := "### Severity Scale\n\n" +
		"- CRITICAL — will take prod down\n- HIGH — should block the merge\n" +
		"- MEDIUM — fix soon\n- LOW — nice to fix\n- NIT — whatever\n"
	if !reflect.DeepEqual(extractCriteria(severityScale), extractCriteria(reworded)) {
		t.Error("tripwire false-fired on pure rephrasing — too brittle for allowed compression")
	}
}

// TestDiffCriteria_DetectsSetChanges proves the map-level comparison used by
// TestPersonaCriteria_Tripwire provably FAILS on the two cases the Reviewer
// needs guaranteed: (a) a change to a persona's severity-tier SET, and (b) a
// change to the persona SET itself (count + identity — add/remove/rename).
func TestDiffCriteria_DetectsSetChanges(t *testing.T) {
	five := []string{"CRITICAL", "HIGH", "LOW", "MEDIUM", "NIT"}
	baseline := map[string]criteriaSig{
		"cfg::Alpha": {SeverityLevels: five},
		"cfg::Beta":  {SeverityLevels: []string{"LOW", "NIT"}},
	}

	cases := []struct {
		name    string
		mutate  func(map[string]criteriaSig) map[string]criteriaSig
		wantSub string // substring expected in at least one diff
	}{
		{
			name: "severity tier dropped from a persona",
			mutate: func(m map[string]criteriaSig) map[string]criteriaSig {
				m["cfg::Alpha"] = criteriaSig{SeverityLevels: []string{"CRITICAL", "HIGH", "LOW", "MEDIUM"}} // NIT gone
				return m
			},
			wantSub: "criteria CHANGED",
		},
		{
			name: "severity tier added to a persona",
			mutate: func(m map[string]criteriaSig) map[string]criteriaSig {
				m["cfg::Beta"] = criteriaSig{SeverityLevels: []string{"LOW", "NIT", "BLOCKER"}}
				return m
			},
			wantSub: "criteria CHANGED",
		},
		{
			name: "persona added",
			mutate: func(m map[string]criteriaSig) map[string]criteriaSig {
				m["cfg::Gamma"] = criteriaSig{SeverityLevels: []string{"HIGH"}}
				return m
			},
			wantSub: "persona ADDED",
		},
		{
			name: "persona removed",
			mutate: func(m map[string]criteriaSig) map[string]criteriaSig {
				delete(m, "cfg::Beta")
				return m
			},
			wantSub: "persona REMOVED",
		},
		{
			name: "persona renamed (add + remove)",
			mutate: func(m map[string]criteriaSig) map[string]criteriaSig {
				m["cfg::BetaRenamed"] = m["cfg::Beta"]
				delete(m, "cfg::Beta")
				return m
			},
			wantSub: "persona", // both ADDED + REMOVED surface
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current := tc.mutate(cloneCriteriaMap(baseline))
			diffs := diffCriteria(baseline, current)
			if len(diffs) == 0 {
				t.Fatalf("diffCriteria FAILED to detect %q — guard is theater", tc.name)
			}
			found := false
			for _, d := range diffs {
				if strings.Contains(d, tc.wantSub) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a diff containing %q for %q, got: %v", tc.wantSub, tc.name, diffs)
			}
		})
	}

	// Control: identical maps must produce NO diffs.
	if diffs := diffCriteria(baseline, cloneCriteriaMap(baseline)); len(diffs) != 0 {
		t.Errorf("diffCriteria false-fired on identical maps: %v", diffs)
	}
}

func cloneCriteriaMap(in map[string]criteriaSig) map[string]criteriaSig {
	return maps.Clone(in)
}

// ---------------------------------------------------------------------------
// golden file helpers
// ---------------------------------------------------------------------------

func writeJSONGolden(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}

func readJSONGolden(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -updategolden to create): %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("parse golden %s: %v", path, err)
	}
}
