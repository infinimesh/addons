package main

// pickGroup chooses the one static group Mender allows.
// A device that also has groupTag would need a second group. The namespace
// group wins, and warn is true.
func pickGroup(namespace, groupTag string, tags []string) (string, bool) {
	if groupTag == "" {
		return namespace, false
	}
	for _, tag := range tags {
		if tag == groupTag {
			return namespace, true
		}
	}
	return namespace, false
}
