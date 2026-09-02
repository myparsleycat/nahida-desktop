package setting

import (
	"context"
	"math"
	"strings"
)

type spec struct {
	def        Definition
	getDefault func(*Setting) any
	fromStored func(*Setting, *string) any
	toStored   func(*Setting, any) string
	normalize  func(*Setting, any) any
	afterSet   func(*Setting, context.Context, any) error
}

func (sp spec) stored(s *Setting, value any) string {
	if sp.toStored != nil {
		return sp.toStored(s, value)
	}
	return storedString(value)
}

func (sp spec) resolved(s *Setting, value any) any {
	if sp.normalize != nil {
		return sp.normalize(s, value)
	}
	return value
}

func boolSpec(def Definition, fallback bool) spec {
	return spec{
		def: def,
		getDefault: func(*Setting) any {
			return fallback
		},
		fromStored: func(_ *Setting, value *string) any {
			return parseBooleanSetting(value, fallback)
		},
		toStored: func(_ *Setting, value any) string {
			return formatBool(asBool(value))
		},
	}
}

func enumSpec(def Definition, fallback string, allowed []string) spec {
	normalize := func(_ *Setting, value any) any {
		return normalizeEnum(asString(value), allowed, fallback)
	}
	return spec{
		def:        def,
		getDefault: func(*Setting) any { return fallback },
		fromStored: func(_ *Setting, value *string) any {
			return normalizeEnum(deref(value), allowed, fallback)
		},
		normalize: normalize,
	}
}

func clampedIntSpec(def Definition, fallback, min, max int) spec {
	fromNumber := func(value float64, ok bool) int {
		if !ok {
			return fallback
		}
		return clampIntegerSetting(value, min, max, fallback)
	}
	return spec{
		def: def,
		getDefault: func(*Setting) any {
			return fallback
		},
		fromStored: func(_ *Setting, value *string) any {
			n, ok := parseJSInt(deref(value))
			return fromNumber(float64(n), ok)
		},
		normalize: func(_ *Setting, value any) any {
			n, ok := asFloat(value)
			return fromNumber(n, ok)
		},
		toStored: func(_ *Setting, value any) string {
			n, ok := asFloat(value)
			return formatInt(fromNumber(n, ok))
		},
	}
}

func buildSpecs() map[string]spec {
	runOnStartup := boolSpec(definitionsByKey[KeyGeneralRunOnStartup], false)
	runOnStartup.afterSet = func(s *Setting, _ context.Context, value any) error {
		enabled, _ := value.(bool)
		return s.opts.Hooks.runOnStartupChanged(enabled)
	}
	logLevel := enumSpec(definitionsByKey[KeyGeneralLogLevel], defaultLogLevel, logLevels)
	logLevel.afterSet = func(s *Setting, _ context.Context, value any) error {
		s.opts.Hooks.logLevelChanged(asString(value))
		return nil
	}
	specs := map[string]spec{
		KeyGeneralRunOnStartup: runOnStartup,
		KeyGeneralLanguage: {
			def: definitionsByKey[KeyGeneralLanguage],
			getDefault: func(s *Setting) any {
				return defaultLanguageFromLocale(s.opts.Locale)
			},
			fromStored: func(s *Setting, value *string) any {
				if value == nil || *value == "" {
					return defaultLanguageFromLocale(s.opts.Locale)
				}
				return *value
			},
			afterSet: func(s *Setting, _ context.Context, value any) error {
				s.opts.Hooks.languageChanged(asString(value))
				return nil
			},
		},
		KeyGeneralAutoUpdateMode: {
			def: definitionsByKey[KeyGeneralAutoUpdateMode],
			getDefault: func(*Setting) any {
				return defaultAutoUpdateMode
			},
			fromStored: func(_ *Setting, value *string) any {
				return normalizeAutoUpdateMode(deref(value))
			},
			normalize: func(_ *Setting, value any) any {
				return normalizeAutoUpdateMode(asString(value))
			},
			afterSet: func(s *Setting, _ context.Context, value any) error {
				s.opts.Hooks.autoUpdateModeChanged(asString(value))
				return nil
			},
		},
		KeyGeneralRunInBackground: boolSpec(definitionsByKey[KeyGeneralRunInBackground], true),
		KeyGeneralDefaultStartPage: {
			def: definitionsByKey[KeyGeneralDefaultStartPage],
			getDefault: func(*Setting) any {
				return defaultStartPage
			},
			fromStored: func(_ *Setting, value *string) any {
				return sanitizeDefaultStartPage(deref(value))
			},
			normalize: func(_ *Setting, value any) any {
				return sanitizeDefaultStartPage(asString(value))
			},
		},
		KeyGeneralLogLevel:                          logLevel,
		KeyGeneralMoveTransferPageWhenStartTransfer: boolSpec(definitionsByKey[KeyGeneralMoveTransferPageWhenStartTransfer], false),
		KeyGeneralPowerSaveBlockInTransfer: {
			def:        definitionsByKey[KeyGeneralPowerSaveBlockInTransfer],
			getDefault: func(*Setting) any { return false },
			fromStored: func(_ *Setting, value *string) any {
				return parseBooleanSetting(value, false)
			},
			toStored: func(_ *Setting, value any) string {
				return formatBool(asBool(value))
			},
			afterSet: func(s *Setting, _ context.Context, _ any) error {
				s.opts.Hooks.powerSaveBlockChanged()
				return nil
			},
		},
		KeyGeneralBisectPreserveD3dx:                 boolSpec(definitionsByKey[KeyGeneralBisectPreserveD3dx], true),
		KeyGeneralTitlebarActivityBadgeClickNavigate: boolSpec(definitionsByKey[KeyGeneralTitlebarActivityBadgeClickNavigate], true),

		KeyModArchiveExtractPathMode:    enumSpec(definitionsByKey[KeyModArchiveExtractPathMode], defaultArchiveExtractPath, archiveExtractPathModes),
		KeyModDeleteArchiveAfterExtract: boolSpec(definitionsByKey[KeyModDeleteArchiveAfterExtract], true),
		KeyModMoveFolderInsteadOfCopy:   boolSpec(definitionsByKey[KeyModMoveFolderInsteadOfCopy], true),
		KeyModSearchModPreview:          boolSpec(definitionsByKey[KeyModSearchModPreview], false),
		KeyModAutoResolveDownloadTarget: boolSpec(definitionsByKey[KeyModAutoResolveDownloadTarget], false),
		KeyModAutoResolveDownloadTargetSources: {
			def: definitionsByKey[KeyModAutoResolveDownloadTargetSources],
			getDefault: func(*Setting) any {
				return append([]string(nil), defaultDownloadSources...)
			},
			fromStored: func(_ *Setting, value *string) any {
				return parseDownloadSources(value)
			},
			normalize: func(_ *Setting, value any) any {
				return normalizeDownloadSources(value)
			},
			toStored: func(_ *Setting, value any) string {
				return encodeJSON(normalizeDownloadSources(value))
			},
		},
		KeyModCopyShaderFixesOnEnable: boolSpec(definitionsByKey[KeyModCopyShaderFixesOnEnable], true),
		KeyModSidebarLayout:           enumSpec(definitionsByKey[KeyModSidebarLayout], defaultSidebarLayout, sidebarLayoutModes),
		KeyModCharacterSidebarWidth: clampedIntSpec(
			definitionsByKey[KeyModCharacterSidebarWidth],
			modCharacterSidebarWidthDefault,
			modCharacterSidebarWidthMin,
			modCharacterSidebarWidthMax,
		),
		KeyModGridLayoutMode: enumSpec(definitionsByKey[KeyModGridLayoutMode], defaultModGridLayout, modGridLayoutModes),
		KeyModGridResponsiveBaseWidth: clampedIntSpec(
			definitionsByKey[KeyModGridResponsiveBaseWidth],
			modGridResponsiveBaseWidthDefault,
			modGridWidthMin,
			modGridWidthMax,
		),
		KeyModGridFixedCardWidth: clampedIntSpec(
			definitionsByKey[KeyModGridFixedCardWidth],
			modGridFixedCardWidthDefault,
			modGridWidthMin,
			modGridWidthMax,
		),
		KeyModGridFixedColumnCount: clampedIntSpec(
			definitionsByKey[KeyModGridFixedColumnCount],
			modGridFixedColumnCountDefault,
			modGridColumnMin,
			modGridColumnMax,
		),
		KeyModDisabledPrefixStyle:             enumSpec(definitionsByKey[KeyModDisabledPrefixStyle], defaultDisabledPrefix, disabledPrefixStyles),
		KeyModReturnToGamebananaAfterDownload: boolSpec(definitionsByKey[KeyModReturnToGamebananaAfterDownload], false),

		KeyToolsTouchProfileLlmProtocol: {
			def: definitionsByKey[KeyToolsTouchProfileLlmProtocol],
			getDefault: func(*Setting) any {
				return defaultTouchProfileLlmProtocol
			},
			fromStored: func(_ *Setting, value *string) any {
				if isTouchProfileLlmProtocol(deref(value)) {
					return deref(value)
				}
				return defaultTouchProfileLlmProtocol
			},
			normalize: func(_ *Setting, value any) any {
				if isTouchProfileLlmProtocol(asString(value)) {
					return asString(value)
				}
				return defaultTouchProfileLlmProtocol
			},
		},
		KeyToolsTouchProfileLlmEndpoint: {
			def: definitionsByKey[KeyToolsTouchProfileLlmEndpoint],
			getDefault: func(s *Setting) any {
				return defaultLLMEndpoint(s.opts.LLMBaseURL)
			},
			fromStored: func(_ *Setting, value *string) any {
				return normalizeTouchProfileLlmEndpoint(deref(value))
			},
			normalize: func(_ *Setting, value any) any {
				return normalizeTouchProfileLlmEndpoint(asString(value))
			},
		},
		KeyToolsTouchProfileLlmModel: {
			def: definitionsByKey[KeyToolsTouchProfileLlmModel],
			getDefault: func(*Setting) any {
				return defaultTouchProfileLlmModel
			},
			fromStored: func(_ *Setting, value *string) any {
				if value == nil {
					return defaultTouchProfileLlmModel
				}
				if trimmed := strings.TrimSpace(deref(value)); trimmed != "" {
					return trimmed
				}
				return defaultTouchProfileLlmModel
			},
			normalize: func(_ *Setting, value any) any {
				if trimmed := strings.TrimSpace(asString(value)); trimmed != "" {
					return trimmed
				}
				return defaultTouchProfileLlmModel
			},
		},
		KeyToolsTouchProfileLlmReasoning: {
			def: definitionsByKey[KeyToolsTouchProfileLlmReasoning],
			getDefault: func(*Setting) any {
				return defaultTouchProfileLlmReasoning
			},
			fromStored: func(_ *Setting, value *string) any {
				if isTouchProfileLlmReasoning(deref(value)) {
					return deref(value)
				}
				return defaultTouchProfileLlmReasoning
			},
			normalize: func(_ *Setting, value any) any {
				if isTouchProfileLlmReasoning(asString(value)) {
					return asString(value)
				}
				return defaultTouchProfileLlmReasoning
			},
		},
		KeyToolsWuwaFixerUpdateNotification: boolSpec(definitionsByKey[KeyToolsWuwaFixerUpdateNotification], true),

		KeyTransferDownloadConcurrency: clampedIntSpec(
			definitionsByKey[KeyTransferDownloadConcurrency],
			transferDownloadConcurrencyDefault,
			transferDownloadConcurrencyMin,
			transferDownloadConcurrencyMax,
		),
		KeyTransferDownloadBandwidthLimitMibps: {
			def: definitionsByKey[KeyTransferDownloadBandwidthLimitMibps],
			getDefault: func(*Setting) any {
				return transferBandwidthDefault
			},
			fromStored: func(_ *Setting, value *string) any {
				n, ok := parseJSInt(deref(value))
				if !ok {
					return transferBandwidthDefault
				}
				return clampIntegerSetting(float64(n), transferBandwidthMin, transferBandwidthMax, transferBandwidthDefault)
			},
			normalize: func(_ *Setting, value any) any {
				n, ok := asFloat(value)
				if !ok {
					return transferBandwidthDefault
				}
				return clampIntegerSetting(n, transferBandwidthMin, transferBandwidthMax, transferBandwidthDefault)
			},
			toStored: func(_ *Setting, value any) string {
				n, ok := asFloat(value)
				if !ok {
					return formatInt(transferBandwidthDefault)
				}
				return formatInt(clampIntegerSetting(n, transferBandwidthMin, transferBandwidthMax, transferBandwidthDefault))
			},
			afterSet: func(s *Setting, _ context.Context, value any) error {
				n, _ := asFloat(value)
				s.opts.Hooks.bandwidthLimitChanged(int(n))
				return nil
			},
		},
		KeyTransferUploadConcurrency: clampedIntSpec(
			definitionsByKey[KeyTransferUploadConcurrency],
			transferUploadConcurrencyDefault,
			transferUploadConcurrencyMin,
			transferUploadConcurrencyMax,
		),

		KeyDriveNameSortPolicy: {
			def: definitionsByKey[KeyDriveNameSortPolicy],
			getDefault: func(*Setting) any {
				return normalizeDriveNameSortPolicy("")
			},
			fromStored: func(_ *Setting, value *string) any {
				return normalizeDriveNameSortPolicy(deref(value))
			},
			normalize: func(_ *Setting, value any) any {
				return normalizeDriveNameSortPolicy(asString(value))
			},
		},
		KeyDriveAutoTryPasswords: boolSpec(definitionsByKey[KeyDriveAutoTryPasswords], false),
		KeyDrivePasswordList: {
			def: definitionsByKey[KeyDrivePasswordList],
			getDefault: func(*Setting) any {
				return []string{}
			},
			fromStored: func(_ *Setting, value *string) any {
				return parsePasswordList(value)
			},
			normalize: func(_ *Setting, value any) any {
				return normalizePasswordList(value)
			},
			toStored: func(_ *Setting, value any) string {
				return encodeJSON(normalizePasswordList(value))
			},
		},

		KeyDebugOpenConsole: {
			def:        definitionsByKey[KeyDebugOpenConsole],
			getDefault: func(*Setting) any { return false },
			fromStored: func(_ *Setting, value *string) any {
				return parseBooleanSetting(value, false)
			},
			toStored: func(_ *Setting, value any) string {
				return formatBool(asBool(value))
			},
			afterSet: func(s *Setting, _ context.Context, value any) error {
				s.opts.Hooks.openConsoleChanged(asBool(value))
				return nil
			},
		},

		KeyModelViewerToneMapping: enumSpec(definitionsByKey[KeyModelViewerToneMapping], defaultToneMapping, modelViewerToneMappings),
		KeyModelViewerEnvironment: enumSpec(definitionsByKey[KeyModelViewerEnvironment], defaultEnvironment, modelViewerEnvironments),
		KeyModelViewerExposure: {
			def: definitionsByKey[KeyModelViewerExposure],
			getDefault: func(*Setting) any {
				return defaultExposure
			},
			fromStored: func(_ *Setting, value *string) any {
				n, ok := parseJSFloat(deref(value))
				if !ok {
					return clampModelViewerExposure(math.NaN())
				}
				return clampModelViewerExposure(n)
			},
			normalize: func(_ *Setting, value any) any {
				n, ok := asFloat(value)
				if !ok {
					return clampModelViewerExposure(math.NaN())
				}
				return clampModelViewerExposure(n)
			},
			toStored: func(_ *Setting, value any) string {
				n, ok := asFloat(value)
				if !ok {
					return formatFloat(defaultExposure)
				}
				return formatFloat(clampModelViewerExposure(n))
			},
		},

		KeyXXMIPersistToggles: {
			def:        definitionsByKey[KeyXXMIPersistToggles],
			getDefault: func(*Setting) any { return false },
			fromStored: func(_ *Setting, value *string) any {
				return parseBooleanSetting(value, false)
			},
			toStored: func(_ *Setting, value any) string {
				return formatBool(asBool(value))
			},
			afterSet: func(s *Setting, ctx context.Context, value any) error {
				enabled := asBool(value)
				if enabled {
					if err := s.Set(ctx, KeyGeneralRunInBackground, true); err != nil {
						return err
					}
				}
				s.opts.Hooks.persistTogglesChanged(enabled)
				return nil
			},
		},
	}
	return specs
}
