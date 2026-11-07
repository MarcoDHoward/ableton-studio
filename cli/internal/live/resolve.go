// Copyright 2026 Marco and contributors. Licensed under Apache-2.0. See LICENSE.

package live

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Candidate is one named object offered back to the user in an error.
type Candidate struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

func (c Candidate) String() string {
	return fmt.Sprintf("%d %q", c.Number, c.Name)
}

func joinCandidates(cs []Candidate) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = c.String()
	}
	return strings.Join(parts, ", ")
}

// NotFoundError: the input matched no object by number or name.
type NotFoundError struct {
	Kind        string
	Input       string
	Count       int
	Suggestions []Candidate
}

func (e *NotFoundError) Error() string {
	msg := fmt.Sprintf("%s %q not found", e.Kind, e.Input)
	switch {
	case e.Count == 0:
		msg += fmt.Sprintf(" (there is no %s to choose from)", e.Kind)
	case len(e.Suggestions) > 0:
		msg += fmt.Sprintf("; close matches: %s", joinCandidates(e.Suggestions))
	default:
		msg += fmt.Sprintf("; use a name or a number from 1 to %d", e.Count)
	}
	return msg
}

// AmbiguousError: the input matched more than one object by name.
type AmbiguousError struct {
	Kind       string
	Input      string
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%s %q matches %d %ss: %s; use the number instead",
		e.Kind, e.Input, len(e.Candidates), e.Kind, joinCandidates(e.Candidates))
}

func fold(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Resolve maps a 1-based number or a name onto a 0-based index into names.
// Order: a number in range wins; then an exact case-insensitive name; then a
// unique prefix; then a unique substring. Several hits at the same stage are
// an AmbiguousError listing every candidate with its number.
func Resolve(kind, input string, names []string) (int, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return -1, &NotFoundError{Kind: kind, Input: input, Count: len(names)}
	}
	if n, err := strconv.Atoi(in); err == nil && n >= 1 && n <= len(names) {
		return n - 1, nil
	}
	key := fold(in)
	stages := []func(string) bool{
		func(name string) bool { return fold(name) == key },
		func(name string) bool { return strings.HasPrefix(fold(name), key) },
		func(name string) bool { return strings.Contains(fold(name), key) },
	}
	for _, match := range stages {
		var hits []Candidate
		for i, name := range names {
			if match(name) {
				hits = append(hits, Candidate{Number: i + 1, Name: name})
			}
		}
		if len(hits) == 1 {
			return hits[0].Number - 1, nil
		}
		if len(hits) > 1 {
			return -1, &AmbiguousError{Kind: kind, Input: input, Candidates: hits}
		}
	}
	return -1, &NotFoundError{Kind: kind, Input: input, Count: len(names), Suggestions: Suggest(in, names, 3)}
}

// Suggest returns up to max names closest to input by edit distance.
func Suggest(input string, names []string, max int) []Candidate {
	key := fold(input)
	type scored struct {
		c    Candidate
		dist int
	}
	var all []scored
	for i, name := range names {
		f := fold(name)
		if f == "" {
			continue
		}
		d := levenshtein(key, f)
		limit := len(key)/2 + 1
		if limit < 2 {
			limit = 2
		}
		if d <= limit {
			all = append(all, scored{Candidate{Number: i + 1, Name: name}, d})
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].dist < all[b].dist })
	if len(all) > max {
		all = all[:max]
	}
	out := make([]Candidate, len(all))
	for i, s := range all {
		out[i] = s.c
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// SendLetter names a send the way Live does: 0 -> "A", 1 -> "B", ...
func SendLetter(i int) string {
	if i < 0 {
		return "?"
	}
	s := ""
	for {
		s = string(rune('A'+i%26)) + s
		i = i/26 - 1
		if i < 0 {
			return s
		}
	}
}

// ResolveSend maps "A".."L" (case-insensitive) or a 1-based number onto a
// 0-based send index, given how many sends (return tracks) exist.
func ResolveSend(input string, count int) (int, error) {
	in := strings.TrimSpace(input)
	names := make([]string, count)
	for i := range names {
		names[i] = SendLetter(i)
	}
	if n, err := strconv.Atoi(in); err == nil {
		if n >= 1 && n <= count {
			return n - 1, nil
		}
		return -1, &NotFoundError{Kind: "send", Input: input, Count: count}
	}
	for i, name := range names {
		if strings.EqualFold(name, in) {
			return i, nil
		}
	}
	return -1, &NotFoundError{Kind: "send", Input: input, Count: count}
}

// ParseSlot parses a 1-based slot/clip number and returns the 0-based index.
func ParseSlot(kind, input string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || n < 1 {
		return -1, fmt.Errorf("%s must be a 1-based number, got %q", kind, input)
	}
	return n - 1, nil
}
