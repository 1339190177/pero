package harness

import point "github.com/phodal/pero/harness/go/runtime"

// SessionArchiveAdmission is the snapshot-only fence published by the workspace
// authority. Native providers resolve it dynamically so mounting a provider
// after a session was archived cannot bypass its admission fence.
type SessionArchiveAdmission interface{ IsArchived(SessionID) bool }

var SessionArchiveAdmissionKey = point.NewKey[SessionArchiveAdmission]("harness.session-archive-admission")
