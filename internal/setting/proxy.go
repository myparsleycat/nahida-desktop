package setting

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const proxyStorageKey = "network_proxy"

type ProxySettings struct {
	Enabled              bool   `json:"enabled"`
	Type                 string `json:"type"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Username             string `json:"username"`
	HasPassword          bool   `json:"hasPassword"`
	RestartRequired      bool   `json:"restartRequired"`
	ConfigurationInvalid bool   `json:"configurationInvalid"`
}

type ProxySettingsInput struct {
	Enabled        bool   `json:"enabled"`
	Type           string `json:"type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PasswordAction string `json:"passwordAction"`
}

type storedProxy struct {
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Credentials string `json:"credentials,omitempty"`
}

func defaultProxy() infra.ProxyConfig { return infra.ProxyConfig{Type: "http"} }

func (s *Setting) readProxy(ctx context.Context) (infra.ProxyConfig, error) {
	if s == nil || s.client == nil {
		return defaultProxy(), errors.New("proxy.storageUnavailable")
	}
	row, err := s.client.Settings.Get(ctx, proxyStorageKey)
	if err != nil {
		return defaultProxy(), err
	}
	if row == nil || row.Value == nil {
		return defaultProxy(), nil
	}
	var stored storedProxy
	if err := json.Unmarshal([]byte(*row.Value), &stored); err != nil {
		return defaultProxy(), errors.New("proxy.configurationInvalid")
	}
	config := infra.ProxyConfig{Enabled: stored.Enabled, Type: stored.Type, Host: stored.Host, Port: stored.Port}
	if stored.Credentials != "" {
		plain, err := platform.NewCrypto().DecryptString(stored.Credentials)
		if err != nil {
			return config, errors.New("proxy.configurationInvalid")
		}
		var credentials struct {
			Username string
			Password string
		}
		if err := json.Unmarshal([]byte(plain), &credentials); err != nil {
			return config, errors.New("proxy.configurationInvalid")
		}
		config.Username, config.Password = credentials.Username, credentials.Password
	}
	if err := config.Validate(); err != nil {
		return config, errors.New("proxy.configurationInvalid")
	}
	return config, nil
}

// LoadProxySettings captures the effective policy once during runtime startup.
//
//wails:ignore
func (s *Setting) LoadProxySettings(ctx context.Context) (infra.ProxyConfig, error) {
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	config, err := s.readProxy(ctx)
	s.activeProxy, s.proxyInvalid = config, err != nil
	return config, infra.AnnotateError(err, infra.Diagnostic{Operation: "proxy", Stage: "load"})
}

func (s *Setting) GetProxySettings(ctx context.Context) (ProxySettings, error) {
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	config, err := s.readProxy(ctx)
	if err != nil {
		// A damaged record must remain repairable from the offline settings page.
		return ProxySettings{Type: "http", ConfigurationInvalid: true}, nil
	}
	return ProxySettings{Enabled: config.Enabled, Type: config.Type, Host: config.Host, Port: config.Port,
		Username: config.Username, HasPassword: config.Password != "", RestartRequired: s.proxyInvalid || config != s.activeProxy}, nil
}

func (s *Setting) SetProxySettings(ctx context.Context, input ProxySettingsInput) error {
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	config := infra.ProxyConfig{Enabled: input.Enabled, Type: input.Type, Host: strings.TrimSpace(input.Host), Port: input.Port, Username: input.Username}
	switch input.PasswordAction {
	case "keep":
		previous, err := s.readProxy(ctx)
		if err != nil {
			return settingError(errors.New("proxy.passwordResetRequired"), "proxy", "read-password", proxyStorageKey, infra.DiagnosticWarn)
		}
		config.Password = previous.Password
	case "replace":
		config.Password = input.Password
	case "clear":
	default:
		return settingError(errors.New("proxy.passwordAction"), "proxy", "validate", proxyStorageKey, infra.DiagnosticWarn)
	}
	if err := config.Validate(); err != nil {
		return settingError(err, "proxy", "validate", proxyStorageKey, infra.DiagnosticWarn)
	}
	stored := storedProxy{Enabled: config.Enabled, Type: config.Type, Host: config.Host, Port: config.Port}
	if config.Username != "" || config.Password != "" {
		plain, err := json.Marshal(struct {
			Username string
			Password string
		}{config.Username, config.Password})
		if err != nil {
			return err
		}
		stored.Credentials, err = platform.NewCrypto().EncryptString(string(plain))
		if err != nil {
			return settingError(err, "proxy", "encrypt", proxyStorageKey, infra.DiagnosticError)
		}
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if s.client == nil {
		return errors.New("proxy.storageUnavailable")
	}
	value := string(encoded)
	if err := s.client.Settings.Upsert(ctx, proxyStorageKey, &value); err != nil {
		return settingError(err, "proxy", "save", proxyStorageKey, infra.DiagnosticError)
	}
	return nil
}
