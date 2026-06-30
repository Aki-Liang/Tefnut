package archive

import (
	"sort"
	"strings"
)

// NatLess reports whether a sorts before b using natural ordering:
// embedded digit runs are compared by numeric value, other runs lexically.
func NatLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ai, bi := a[i], b[j]
		if isDigit(ai) && isDigit(bi) {
			as, ae := digitRun(a, i)
			bs, be := digitRun(b, j)
			an := strings.TrimLeft(a[as:ae], "0")
			bn := strings.TrimLeft(b[bs:be], "0")
			if len(an) != len(bn) {
				return len(an) < len(bn)
			}
			if an != bn {
				return an < bn
			}
			i, j = ae, be
			continue
		}
		if ai != bi {
			return ai < bi
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func digitRun(s string, start int) (int, int) {
	end := start
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	return start, end
}

// SortNatural sorts names in place using NatLess.
func SortNatural(names []string) {
	sort.Slice(names, func(i, j int) bool { return NatLess(names[i], names[j]) })
}
