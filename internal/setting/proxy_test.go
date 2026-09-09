package setting

import (
	"context"
	"strings"
	"testing"
)

func TestProxyCredentialsAndRestart(t *testing.T) {
	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	if _, err := s.LoadProxySettings(ctx); err != nil {
		t.Fatal(err)
	}
	input := ProxySettingsInput{
		Enabled:        true,
		Type:           "socks5h",
		Host:           "127.0.0.1",
		Port:           1080,
		Username:       "private-user",
		Password:       "private-password",
		PasswordAction: "replace",
	}
	if err := s.SetProxySettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	raw := rawValue(t, s, proxyStorageKey)
	if strings.Contains(raw, input.Username) || strings.Contains(raw, input.Password) {
		t.Fatal("plaintext credentials in database")
	}
	view, err := s.GetProxySettings(ctx)
	if err != nil || !view.HasPassword || !view.RestartRequired || view.Username != input.Username {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if s.activeProxy.Enabled {
		t.Fatal("save changed active policy")
	}
	input.PasswordAction, input.Password, input.Port = "keep", "ignored", 1081
	if err := s.SetProxySettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	config, err := s.LoadProxySettings(ctx)
	if err != nil || config.Password != "private-password" || config.Port != 1081 {
		t.Fatal("password was not preserved")
	}
	view, _ = s.GetProxySettings(ctx)
	if view.RestartRequired {
		t.Fatal("restart still required after startup load")
	}
	input.Enabled, input.Username, input.PasswordAction = false, "", "clear"
	if err := s.SetProxySettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	view, _ = s.GetProxySettings(ctx)
	if view.HasPassword || view.Username != "" || !view.RestartRequired {
		t.Fatalf("clear: %+v", view)
	}
}

func TestProxyValidationPreservesStoredConfig(t *testing.T) {
	s, _ := openTemp(t, Options{})
	ctx := context.Background()
	input := ProxySettingsInput{Enabled: true, Type: "http", Host: "localhost", Port: 8080, PasswordAction: "clear"}
	if err := s.SetProxySettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	previous := rawValue(t, s, proxyStorageKey)
	for _, change := range []func(*ProxySettingsInput){
		func(p *ProxySettingsInput) { p.Port = 0 }, func(p *ProxySettingsInput) { p.Type = "ftp" },
		func(p *ProxySettingsInput) { p.Host = "http://localhost" }, func(p *ProxySettingsInput) { p.PasswordAction = "other" },
	} {
		next := input
		change(&next)
		if err := s.SetProxySettings(ctx, next); err == nil {
			t.Fatal("accepted invalid settings")
		}
		if rawValue(t, s, proxyStorageKey) != previous {
			t.Fatal("invalid save changed storage")
		}
	}
	if err := s.AdvancedSet(ctx, proxyStorageKey, "{}"); err == nil {
		t.Fatal("advanced settings bypassed validation")
	}
	if err := s.SetProxySettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	rows, err := s.AdvancedGetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Key == proxyStorageKey {
			t.Fatal("proxy credential blob listed in advanced settings")
		}
	}
}

func TestDamagedProxyRemainsRepairable(t *testing.T) {
	for _, value := range []string{`broken`, `{"enabled":true,"type":"http","host":"localhost","port":8080,"credentials":"not-dpapi"}`} {
		t.Run(value, func(t *testing.T) {
			s, _ := openTemp(t, Options{})
			ctx := context.Background()
			if err := s.client.Settings.Upsert(ctx, proxyStorageKey, &value); err != nil {
				t.Fatal(err)
			}
			if _, err := s.LoadProxySettings(ctx); err == nil {
				t.Fatal("accepted damaged record")
			}
			view, err := s.GetProxySettings(ctx)
			if err != nil || !view.ConfigurationInvalid {
				t.Fatal("cannot display repair UI")
			}
			input := ProxySettingsInput{Type: "http", PasswordAction: "clear"}
			if err := s.SetProxySettings(ctx, input); err != nil {
				t.Fatal(err)
			}
			view, _ = s.GetProxySettings(ctx)
			if view.ConfigurationInvalid || !view.RestartRequired {
				t.Fatal("repair must require restart")
			}
		})
	}
}
