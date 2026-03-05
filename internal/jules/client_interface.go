package jules

type ClientInterface interface {
	ListSources() ([]Source, error)
	ListSessions() ([]Session, error)
	GetSession(sessionName string) (*Session, error)
	ListActivities(sessionName string, sinceID string) ([]Activity, error)
	ListAllActivities(sessionName string) ([]Activity, error)
	SendMessage(sessionName, message string) error
	CreateSession(prompt, source, mode string) (*Session, error)
	ArchiveSession(sessionName string) error
	ApprovePlan(sessionName string) error
}
