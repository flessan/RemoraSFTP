package protocol

// CopyPolicy controls how copy/move operations resolve a destination that
// already exists. It is shared by the remote connection manager and the
// local filesystem service so both sides behave identically.
type CopyPolicy string

const (
	// PolicyRefuse fails the operation when the destination exists (used
	// when the caller has not resolved conflicts up front).
	PolicyRefuse CopyPolicy = "refuse"
	// PolicyReplace overwrites the existing destination.
	PolicyReplace CopyPolicy = "replace"
	// PolicySkip leaves the existing destination untouched.
	PolicySkip CopyPolicy = "skip"
	// PolicyRename copies to a non-colliding name, e.g. "file (1).txt".
	PolicyRename CopyPolicy = "rename"
)

// ParseCopyPolicy validates a policy coming from the API.
func ParseCopyPolicy(p string) (CopyPolicy, bool) {
	switch CopyPolicy(p) {
	case PolicyRefuse, PolicyReplace, PolicySkip, PolicyRename:
		return CopyPolicy(p), true
	case "":
		return PolicyRefuse, true
	}
	return "", false
}
