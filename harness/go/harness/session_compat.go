package harness

import (
	"log/slog"

	harnesssession "github.com/phodal/pero/harness/go/session"
)

var (
	ErrSessionDurabilityAttached  = harnesssession.ErrSessionDurabilityAttached
	ErrSessionAlreadyOwned        = harnesssession.ErrSessionAlreadyOwned
	ErrSessionPersistenceConflict = harnesssession.ErrSessionPersistenceConflict
	ErrSessionChanged             = harnesssession.ErrSessionChanged
	ErrSessionForkUnavailable     = harnesssession.ErrSessionForkUnavailable
	ErrInvalidSessionForkAnchor   = harnesssession.ErrInvalidSessionForkAnchor
)

const SessionFormatVersion = harnesssession.SessionFormatVersion

type Session = harnesssession.Session
type SessionStore = harnesssession.SessionStore
type SessionStoreEventKind = harnesssession.SessionStoreEventKind
type SessionStoreEvent = harnesssession.SessionStoreEvent
type SessionPersistence = harnesssession.SessionPersistence
type SessionIdentityPersistence = harnesssession.SessionIdentityPersistence
type SessionLocation = harnesssession.SessionLocation
type SessionPersistenceLocator = harnesssession.SessionPersistenceLocator
type SessionPersistenceCandidate = harnesssession.SessionPersistenceCandidate
type SessionCatalogPersistence = harnesssession.SessionCatalogPersistence
type JSONLSessionPersistence = harnesssession.JSONLSessionPersistence
type JSONLPersistenceOptions = harnesssession.JSONLPersistenceOptions
type RequestHeaderEvent = harnesssession.RequestHeaderEvent
type RequestContext = harnesssession.RequestContext
type InboxTarget = harnesssession.InboxTarget
type InboxMutation = harnesssession.InboxMutation
type DurableInbox = harnesssession.DurableInbox
type inboxSpliceEvent = harnesssession.InboxSpliceEvent
type ProjectionWireDefinition = harnesssession.ProjectionWireDefinition
type ProjectionDefinition = harnesssession.ProjectionDefinition
type ProjectionSpec[S, V any] = harnesssession.ProjectionSpec[S, V]
type HostProjectionSpec[S any] = harnesssession.HostProjectionSpec[S]
type ProjectionChangeListener = harnesssession.ProjectionChangeListener
type ProjectionSnapshot = harnesssession.ProjectionSnapshot
type ProjectionCheckpointRow = harnesssession.ProjectionCheckpointRow
type ProjectionCheckpoint = harnesssession.ProjectionCheckpoint
type SessionProjectionRegistry = harnesssession.SessionProjectionRegistry
type ProjectionRestoreResult = harnesssession.ProjectionRestoreResult

const (
	SessionStoreAdded   = harnesssession.SessionStoreAdded
	SessionStoreRemoved = harnesssession.SessionStoreRemoved
	InboxNextTurn       = harnesssession.InboxNextTurn
	InboxNextStep       = harnesssession.InboxNextStep
)

func NewSession(id SessionID, cwd string) *Session { return harnesssession.NewSession(id, cwd) }
func NewDurableInbox(session *Session) (*DurableInbox, error) {
	return harnesssession.NewDurableInbox(session)
}
func RestoreSession(header SessionHeader, events []Event) (*Session, error) {
	return harnesssession.RestoreSession(header, events)
}
func NewSessionStore() *SessionStore { return harnesssession.NewSessionStore() }
func NewJSONLSessionPersistence(dir string, options ...JSONLPersistenceOptions) (*JSONLSessionPersistence, error) {
	return harnesssession.NewJSONLSessionPersistence(dir, options...)
}
func NewSessionProjectionRegistry(store *SessionStore, logger *slog.Logger) (*SessionProjectionRegistry, error) {
	return harnesssession.NewSessionProjectionRegistry(store, logger)
}
func DefineProjection[S, V any](spec ProjectionSpec[S, V]) ProjectionDefinition {
	return harnesssession.DefineProjection(spec)
}
func DefineHostProjection[S any](spec HostProjectionSpec[S]) ProjectionDefinition {
	return harnesssession.DefineHostProjection(spec)
}
func ProjectionState[S any](registry *SessionProjectionRegistry, session *Session, key string) (S, bool, error) {
	return harnesssession.ProjectionState[S](registry, session, key)
}

func inspectCompactionLog(events []Event) (bool, error) {
	return harnesssession.InspectCompactionLog(events)
}

func inspectOpenTurn(events []Event) (*int, error) {
	return harnesssession.InspectOpenTurn(events)
}

func decodeSurfaceMessage(event Event) (Message, error) {
	return harnesssession.DecodeSurfaceMessage(event)
}

func isSurfaceEventType(eventType string) bool {
	return harnesssession.IsSurfaceEventType(eventType)
}

func validateDurableMessage(message Message) (Message, error) {
	return harnesssession.ValidateDurableMessage(message)
}

func messagesEqual(left, right Message) bool {
	return harnesssession.MessagesEqual(left, right)
}
