package app

import (
	"context"
	"time"

	"nahida.live/desktop/internal/infra"
)

func (rt *runtime) initProxy(ctx context.Context, configureBrowserArguments func([]string) error) error {
	config, err := rt.setting.LoadProxySettings(ctx)
	var network *infra.ProxyNetwork
	if err == nil {
		network, err = infra.NewProxyNetwork(config)
	}
	if err != nil {
		_ = infra.ReportError(rt.log, err, "Proxy", infra.Diagnostic{Operation: "startup", Stage: "load-configuration"})
		rt.http.UseTransport(infra.BlockedProxyTransport{})
	} else {
		rt.http.UseTransport(network.HTTPTransport())
	}
	relay, err := infra.StartProxyRelay(network, rt.log)
	if err != nil {
		return err
	}
	rt.proxyRelay = relay
	if configureBrowserArguments == nil {
		return nil
	}
	args := windowsApplicationOptions().AdditionalBrowserArgs
	args = append(args, relay.BrowserArguments()...)
	return configureBrowserArguments(args)
}

// proxyFailureSource binds the relay now: Close clears rt.proxyRelay while
// window callbacks may still be reading failures on another goroutine.
func (rt *runtime) proxyFailureSource() func(time.Time) []infra.ProxyFailureSummary {
	if rt.proxyRelay == nil {
		return nil
	}
	return rt.proxyRelay.FailuresSince
}
