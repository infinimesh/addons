package otacore

import "sort"

// DiffGroups returns tags that next adds or prev drops. Order does not matter.
func DiffGroups(prev, next []string) (add, remove []string) {
	have := map[string]struct{}{}
	want := map[string]struct{}{}
	for _, t := range prev {
		if t != "" {
			have[t] = struct{}{}
		}
	}
	for _, t := range next {
		if t != "" {
			want[t] = struct{}{}
		}
	}
	for t := range want {
		if _, ok := have[t]; !ok {
			add = append(add, t)
		}
	}
	for t := range have {
		if _, ok := want[t]; !ok {
			remove = append(remove, t)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	return add, remove
}
