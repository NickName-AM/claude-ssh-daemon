// Package guard provides a prompt-injection detection engine for scanning
// SSH tool output. It exposes package-level functions (no constructor, no state)
// that classify text into one or more of four built-in threat categories and
// an optional set of caller-supplied custom patterns.
//
// Security invariant: matched text is NEVER included in the return value.
// Only the category label and match count are returned. This prevents the
// guard layer from accidentally echoing adversarial content back to the LLM.
package guard

import (
	"regexp"
	"strings"
	"unicode"
)

// Result holds the outcome of a scan. Matches is nil when no injection
// patterns are detected (zero allocation on the happy path).
type Result struct {
	Matches []Match
}

// Match describes one category of injection signal detected in the text.
// Category is one of the four built-in labels ("xml_tool_tags",
// "authority_tags", "instruction_override", "role_hijacking") or "custom"
// for caller-supplied extra patterns. Count is the number of non-overlapping
// occurrences across the full text.
type Match struct {
	Category string
	Count    int
}

// Package-level pre-compiled patterns. All patterns are compiled once at
// program startup (MustCompile panics on invalid authored patterns rather
// than silently returning nil). The (?i) inline flag enables case-insensitive
// matching (D-03). All regex strings are verified with go run probes.
var (
	// reXMLToolTags matches XML-like LLM tool invocation tags used by Claude,
	// OpenAI function calling, and similar frameworks.
	// Covers: <tool_call>, </tool_call>, <function_calls>, </function_calls>,
	//         <invoke>, </invoke>, <parameter name="...">, <result>, </result>
	// Does NOT match: <a href="...">, <img>, <div>, <toolbox>
	reXMLToolTags = regexp.MustCompile(
		`(?i)</?(?:tool_call|function_calls|invoke|parameter|result)\b[^>]*>`)

	// reAuthorityTags matches authority-assertion tags that attempt to grant
	// elevated trust to an instruction block.
	// Covers: <system>, </system>, <admin>, </admin>, <assistant>, </assistant>,
	//         <prompt>, </prompt> (with optional whitespace before >)
	// Does NOT match: "system configuration", "the assistant responded"
	reAuthorityTags = regexp.MustCompile(
		`(?i)</?(?:system|admin|assistant|prompt)\s*>`)

	// reInstructionOverride matches instruction-override phrases that attempt
	// to replace or nullify prior instructions.
	// Covers: "ignore previous instructions", "Ignore Previous Instructions",
	//         "ignore the previous instructions", "ignore your previous instructions",
	//         "disregard your instructions", "forget all previous instructions"
	// Does NOT match: "follow your instructions", "these are the instructions",
	//         "ignore instructions" (bare verb form — too broad for build/tool output),
	//         "parser will ignore instructions after this point"
	//
	// Pattern has two arms:
	//   1. "previous" required (with optional "all", "the", or "your" before it)
	//      — covers the canonical "ignore [all|the|your] previous instructions" family
	//   2. "your instructions" explicit — preserves "disregard your instructions"
	//      as a true positive without reintroducing the bare-verb false-positive surface
	reInstructionOverride = regexp.MustCompile(
		`(?i)\b(?:ignore|disregard|forget)\s+(?:(?:all\s+)?(?:the\s+|your\s+)?previous\s+instructions|(?:all\s+)?your\s+instructions)\b`)

	// reRoleHijacking matches role/persona hijacking openers that attempt to
	// reassign the model's identity.
	// Covers: "you are now a X", "you are now an X", "act as a X", "act as an X",
	//         "act as if ...", "your new persona is", "your new role is",
	//         "your new identity is"
	// Does NOT match: "you are now connected", "you are now running as sudo",
	//         "you are helpful", "act accordingly", "act as expected",
	//         "act as required" — article/if anchor required after "act as" (Pitfall 5),
	//         and "you are now" requires a following article (a/an) to avoid false
	//         positives on routine SSH/PAM/provisioning status messages.
	reRoleHijacking = regexp.MustCompile(
		`(?i)\b(?:you\s+are\s+now\s+(?:a|an)\s+\w+\b|act\s+as\s+(?:if\b|a\b|an\b)|your\s+new\s+(?:persona|role|identity)\s+is\b)`)
)

// builtinEntry pairs a compiled regexp with its category label.
// Using a single struct slice (instead of parallel slices) ensures the
// pairing is a single unit — a contributor cannot add a pattern without
// a category string without getting a compile error.
type builtinEntry struct {
	re       *regexp.Regexp
	category string
}

// builtins is the index of all built-in detection patterns.
var builtins = []builtinEntry{
	{reXMLToolTags, "xml_tool_tags"},
	{reAuthorityTags, "authority_tags"},
	{reInstructionOverride, "instruction_override"},
	{reRoleHijacking, "role_hijacking"},
}

// ScanWithPatterns scans text against all built-in patterns and any extra
// pre-compiled patterns supplied by the caller. Built-in pattern matches are
// reported per-category. All extra pattern matches are folded into a single
// Match{Category: "custom", Count: N} entry (D-08).
//
// The returned Result has Matches == nil when no patterns hit (D-02, Pitfall 3).
// Matched text is never stored or returned — only the count (D-02).
func ScanWithPatterns(text string, extra []*regexp.Regexp) Result {
	// Normalize Unicode whitespace to ASCII space so that non-ASCII space
	// code points (U+00A0, U+2002, U+2003, U+202F, etc.) cannot bypass
	// multi-word patterns. Go RE2 \s only covers ASCII whitespace.
	text = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)

	var matches []Match // nil zero value — no allocation on the happy path

	for _, b := range builtins {
		count := len(b.re.FindAllStringIndex(text, -1))
		if count > 0 {
			matches = append(matches, Match{
				Category: b.category,
				Count:    count,
			})
		}
	}

	// All extra patterns contribute to a single "custom" entry (D-08).
	if len(extra) > 0 {
		customCount := 0
		for _, re := range extra {
			customCount += len(re.FindAllStringIndex(text, -1))
		}
		if customCount > 0 {
			matches = append(matches, Match{Category: "custom", Count: customCount})
		}
	}

	return Result{Matches: matches}
}

// Scan scans text against the four built-in injection detection patterns.
// It is the primary entry point for the built-in-only case (D-01).
// Phase 5 and Phase 6 use ScanWithPatterns for augmented calls.
func Scan(text string) Result {
	return ScanWithPatterns(text, nil)
}
