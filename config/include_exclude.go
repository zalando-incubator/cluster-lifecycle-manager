package config

import "regexp"

const DefaultInclude = ".*"
const DefaultExclude = "^$"

var DefaultFilter = IncludeExcludeFilter{
	Include: regexp.MustCompile(DefaultInclude),
	Exclude: regexp.MustCompile(DefaultExclude),
}

// IncludeExcludeFilter is a filter consiting of two regular
// expressions.  It can be used to decide if a certain item is allowed
// based on the filters.
type IncludeExcludeFilter struct {
	Include *regexp.Regexp
	Exclude *regexp.Regexp
}

// Allowed returns true if the item is allowed based on the
// IncludeExcludeFilter object. Exclude will trump the include.
func (f *IncludeExcludeFilter) Allowed(item string) bool {
	return !f.Exclude.MatchString(item) && f.Include.MatchString(item)
}
