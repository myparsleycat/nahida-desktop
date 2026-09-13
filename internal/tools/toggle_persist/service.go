package togglepersist

import (
	"sync"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi"
)

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	Settings  any
	XXMI      *xxmi.XXMI
}

type Service struct {
	log       *infra.Log
	emit      func(string, ...any)
	settings  any
	xxmi      *xxmi.XXMI
	persistMu sync.Mutex
	persist   *persistEngine
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	t := &Service{
		log:      opts.Log,
		emit:     opts.EventEmit,
		settings: opts.Settings,
		xxmi:     opts.XXMI,
		persist:  newPersistEngine(),
	}
	t.persist.emit = func(logs []string) { t.emitEvent("setting:xxmi:persistLogs", logs) }
	t.persist.infoFn = func(message string) {
		if t.log != nil {
			t.log.Info(message, "TogglePersist")
		}
	}
	var persistDiagnostics infra.DiagnosticThrottle
	t.persist.diagnosticFn = func(err error, message string) {
		persistDiagnostics.Report(
			t.log,
			err,
			"TogglePersist",
			infra.Diagnostic{
				Operation: "toggle-persist",
				Stage:     "background",
				Fields:    map[string]any{"context": message},
			},
		)
	}
	t.persist.errorFn = func(message string) {
		if t.log != nil {
			t.log.Error(message, "TogglePersist")
		}
	}
	return t
}

func (t *Service) emitEvent(name string, data any) {
	if t != nil && t.emit != nil {
		t.emit(name, data)
	}
}

func (t *Service) Shutdown() error {
	if t == nil {
		return nil
	}
	return t.shutdownPersistWatcher()
}
