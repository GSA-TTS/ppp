package policy

// globMatch reports whether name matches pattern, where '*' matches any run of
// characters (including none) and '?' matches exactly one character. Both
// arguments are expected to already be lowercased by the caller. It uses an
// iterative backtracking walk so it never recurses on adversarial input.
func globMatch(pattern, name string) bool {
	var (
		p, n      int
		star      = -1
		starMatch int
	)
	for n < len(name) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == name[n]):
			p++
			n++
		case p < len(pattern) && pattern[p] == '*':
			star = p
			starMatch = n
			p++
		case star >= 0:
			p = star + 1
			starMatch++
			n = starMatch
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
