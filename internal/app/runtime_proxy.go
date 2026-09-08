package app

import (
	"context"

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
		rt.http.UseTransport(network.Transport)
	}
	relay, err := infra.StartProxyRelay(network, rt.log)
	if err != nil {
		return err
	}
	rt.proxyRelay = relay
	if configureBrowserArguments == nil {
		return nil
	}
	args := append(windowsApplicationOptions().AdditionalBrowserArgs, relay.BrowserArguments()...)
	return configureBrowserArguments(args)
}
