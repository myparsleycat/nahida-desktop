package app

import (
	"context"
	"strings"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

func (rt *runtime) initProxy(ctx context.Context) error {
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
	if rt.tools != nil {
		rt.tools.UseProxyURL(relay.URL())
	}
	return nil
}

// configureGameBananaLogin gives the login windows the WebView2 profile folder and the proxy relay. A
// profile folder that cannot be created leaves fairy's default folder in use.
func (rt *runtime) configureGameBananaLogin(icon []byte, font string, dark func() bool) {
	opts := loginWindowOptions{Icon: icon, Font: font, Dark: dark}
	if rt.proxyRelay != nil {
		opts.BrowserArgs = strings.Join(rt.proxyRelay.BrowserArguments(), " ")
	}
	if rt.appData != nil {
		folder, err := rt.appData.EnsureDir(appdata.WebViewDir)
		if err != nil {
			_ = infra.ReportError(rt.log, err, "GameBananaLogin", infra.Diagnostic{
				Operation: "startup", Stage: "webview-profile",
			})
		}
		opts.UserDataFolder = folder
	}
	rt.gameBananaLogin.Configure(opts, rt.log)
}
