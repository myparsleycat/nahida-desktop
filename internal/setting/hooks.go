package setting

// Hooks are optional afterSet side effects owned by runtime services.
// Empty Hooks are a no-op so settings remain usable in tests and headless callers.
type Hooks struct {
	AfterSet                             func(key string, value any)
	AfterRunOnStartupChanged             func(enabled bool) error
	AfterLanguageChanged                 func(language string)
	AfterAutoUpdateModeChanged           func(mode string)
	AfterIncludePrereleaseChanged        func(enabled bool)
	AfterLogLevelChanged                 func(level string)
	AfterPowerSaveBlockChanged           func()
	AfterBandwidthLimitChanged           func(mibps int)
	AfterOpenConsoleChanged              func(enabled bool)
	AfterWindowBackdropChanged           func(enabled bool)
	AfterPersistTogglesChanged           func(enabled bool)
	AfterToggleViewerAutoGenerateChanged func(enabled bool)
	AfterToggleViewerHotkeyChanged       func(hotkey string)
	AfterElevatedHelperChanged           func(enabled bool)
	AfterRendererReload                  func()
}

func (h Hooks) elevatedHelperChanged(enabled bool) {
	if h.AfterElevatedHelperChanged != nil {
		h.AfterElevatedHelperChanged(enabled)
	}
}

func (h Hooks) runOnStartupChanged(enabled bool) error {
	if h.AfterRunOnStartupChanged != nil {
		return h.AfterRunOnStartupChanged(enabled)
	}
	return nil
}

func (h Hooks) set(key string, value any) {
	if h.AfterSet != nil {
		h.AfterSet(key, value)
	}
}

func (h Hooks) languageChanged(language string) {
	if h.AfterLanguageChanged != nil {
		h.AfterLanguageChanged(language)
	}
}

func (h Hooks) autoUpdateModeChanged(mode string) {
	if h.AfterAutoUpdateModeChanged != nil {
		h.AfterAutoUpdateModeChanged(mode)
	}
}

func (h Hooks) includePrereleaseChanged(enabled bool) {
	if h.AfterIncludePrereleaseChanged != nil {
		h.AfterIncludePrereleaseChanged(enabled)
	}
}

func (h Hooks) logLevelChanged(level string) {
	if h.AfterLogLevelChanged != nil {
		h.AfterLogLevelChanged(level)
	}
}

func (h Hooks) powerSaveBlockChanged() {
	if h.AfterPowerSaveBlockChanged != nil {
		h.AfterPowerSaveBlockChanged()
	}
}

func (h Hooks) bandwidthLimitChanged(mibps int) {
	if h.AfterBandwidthLimitChanged != nil {
		h.AfterBandwidthLimitChanged(mibps)
	}
}

func (h Hooks) openConsoleChanged(enabled bool) {
	if h.AfterOpenConsoleChanged != nil {
		h.AfterOpenConsoleChanged(enabled)
	}
}

func (h Hooks) windowBackdropChanged(enabled bool) {
	if h.AfterWindowBackdropChanged != nil {
		h.AfterWindowBackdropChanged(enabled)
	}
}

func (h Hooks) persistTogglesChanged(enabled bool) {
	if h.AfterPersistTogglesChanged != nil {
		h.AfterPersistTogglesChanged(enabled)
	}
}

func (h Hooks) toggleViewerAutoGenerateChanged(enabled bool) {
	if h.AfterToggleViewerAutoGenerateChanged != nil {
		h.AfterToggleViewerAutoGenerateChanged(enabled)
	}
}

func (h Hooks) toggleViewerHotkeyChanged(hotkey string) {
	if h.AfterToggleViewerHotkeyChanged != nil {
		h.AfterToggleViewerHotkeyChanged(hotkey)
	}
}

func (h Hooks) rendererReload() {
	if h.AfterRendererReload != nil {
		h.AfterRendererReload()
	}
}
