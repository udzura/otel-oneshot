package filter

import (
	"fmt"
	"regexp"
)

// MatchMode values accepted by compileMatcher.
const (
	MatchRegex = "regex"
	MatchExact = "exact"
)

// matcher tests span names against a set of patterns. A nil *matcher matches
// nothing, so callers can treat "no patterns" as a nil matcher.
type matcher struct {
	exact map[string]struct{} // set when mode == exact
	res   []*regexp.Regexp    // set when mode == regex
}

// compileMatcher builds a matcher for the given patterns and mode. It returns
// (nil, nil) when there are no patterns, so an unused fold/hide option costs
// nothing. An empty mode is treated as "regex".
func compileMatcher(patterns []string, mode string) (*matcher, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	switch mode {
	case MatchExact:
		set := make(map[string]struct{}, len(patterns))
		for _, p := range patterns {
			set[p] = struct{}{}
		}
		return &matcher{exact: set}, nil

	case MatchRegex, "":
		res := make([]*regexp.Regexp, 0, len(patterns))
		for _, p := range patterns {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, fmt.Errorf("invalid regex %q: %w", p, err)
			}
			res = append(res, re)
		}
		return &matcher{res: res}, nil

	default:
		return nil, fmt.Errorf("invalid match mode %q (want %s|%s)", mode, MatchExact, MatchRegex)
	}
}

// match reports whether name matches any pattern. A nil receiver never matches.
func (m *matcher) match(name string) bool {
	if m == nil {
		return false
	}
	if m.exact != nil {
		_, ok := m.exact[name]
		return ok
	}
	for _, re := range m.res {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}
