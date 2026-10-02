package metadata

// ReserveTransaction reserves mod directories using the same queue as metadata edits.
// Release must be called exactly once. While reserved, callers must use raw file IO;
// calling Read, Write, Update or other metadata operations would deadlock.
func ReserveTransaction(modPaths ...string) (release func(), err error) {
	return reserve(modPaths...)
}
