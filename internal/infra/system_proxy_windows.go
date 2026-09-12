package infra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	systemWinHTTP           = windows.NewLazySystemDLL("winhttp.dll")
	winHTTPGetIEProxyConfig = systemWinHTTP.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	winHTTPOpen             = systemWinHTTP.NewProc("WinHttpOpen")
	winHTTPCloseHandle      = systemWinHTTP.NewProc("WinHttpCloseHandle")
	winHTTPSetTimeouts      = systemWinHTTP.NewProc("WinHttpSetTimeouts")
	winHTTPGetProxyForURL   = systemWinHTTP.NewProc("WinHttpGetProxyForUrlEx")
	winHTTPCreateResolver   = systemWinHTTP.NewProc("WinHttpCreateProxyResolver")
	winHTTPSetCallback      = systemWinHTTP.NewProc("WinHttpSetStatusCallback")
	winHTTPGetResult        = systemWinHTTP.NewProc("WinHttpGetProxyResult")
	winHTTPFreeResult       = systemWinHTTP.NewProc("WinHttpFreeProxyResult")
	proxyCallbacks          sync.Map
	proxyCallbackID         atomic.Uint64
	proxyCallback           = windows.NewCallback(
		func(handle, id, status uintptr, info unsafe.Pointer, size uintptr) uintptr {
			value, ok := proxyCallbacks.Load(id)
			if !ok {
				return 0
			}
			var err error
			if status == 0x200000 {
				result := (*winHTTPAsyncResult)(info)
				err = syscall.Errno(result.code)
			} else if status != 0x1000000 {
				return 0
			}
			select {
			case value.(chan error) <- err:
			default:
			}
			return 0
		},
	)
	systemProxyGlobalFree = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")

	// Bound concurrent native PAC resolutions; cancellation closes the resolver.
	windowsAutoProxySlots = make(chan struct{}, 4)
)

type winHTTPUserProxyConfig struct {
	autoDetect    int32
	autoConfigURL *uint16
	proxy         *uint16
	bypass        *uint16
}

type winHTTPAutoProxyOptions struct {
	flags           uint32
	autoDetectFlags uint32
	autoConfigURL   *uint16
	reserved        uintptr
	reservedFlags   uint32
	autoLogon       int32
}

type winHTTPAsyncResult struct {
	result uintptr
	code   uint32
}

type winHTTPProxyEntry struct {
	proxy  int32
	_      int32 // fBypass is only meaningful for DIRECT entries.
	scheme int32
	host   *uint16
	port   uint16
}

type winHTTPProxyResult struct {
	count   uint32
	entries *winHTTPProxyEntry
}

func readWindowsProxyConfig() (systemProxyConfig, error) {
	var native winHTTPUserProxyConfig
	ok, _, err := winHTTPGetIEProxyConfig.Call(uintptr(unsafe.Pointer(&native)))
	defer freeWindowsProxyStrings(native.autoConfigURL, native.proxy, native.bypass)
	if ok == 0 {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return systemProxyConfig{}, nil
		}
		return systemProxyConfig{}, err
	}
	return systemProxyConfig{
		autoDetect:    native.autoDetect != 0,
		autoConfigURL: windows.UTF16PtrToString(native.autoConfigURL),
		proxy:         windows.UTF16PtrToString(native.proxy),
		bypass:        windows.UTF16PtrToString(native.bypass),
	}, nil
}

func resolveWindowsAutoProxy(ctx context.Context, target *url.URL, config systemProxyConfig) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case windowsAutoProxySlots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-windowsAutoProxySlots }()
	address := *target
	address.User, address.Fragment = nil, ""
	if address.Scheme == "https" {
		address.Path, address.RawPath, address.RawQuery = "/", "", ""
	}
	return windowsAutoProxy(ctx, address.String(), config)
}

func windowsAutoProxy(ctx context.Context, target string, config systemProxyConfig) (string, error) {
	// WinHTTP's service caches PAC downloads. Keep autoLogon false unless the
	// PAC server challenges, as recommended by the WinHTTP AutoProxy API.
	session, _, err := winHTTPOpen.Call(0, 1 /* WINHTTP_ACCESS_TYPE_NO_PROXY */, 0, 0, 0x10000000 /* ASYNC */)
	if session == 0 {
		return "", fmt.Errorf("WinHttpOpen: %w", err)
	}
	defer func() { _, _, _ = winHTTPCloseHandle.Call(session) }()
	if ok, _, err := winHTTPSetTimeouts.Call(session, 5000, 5000, 5000, 5000); ok == 0 {
		return "", fmt.Errorf("WinHttpSetTimeouts: %w", err)
	}
	address, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", errors.New("invalid URL for Windows proxy resolution")
	}
	var options winHTTPAutoProxyOptions
	if config.autoConfigURL != "" {
		options.flags = 2 // WINHTTP_AUTOPROXY_CONFIG_URL
		options.autoConfigURL, err = windows.UTF16PtrFromString(config.autoConfigURL)
		if err != nil {
			return "", errors.New("invalid Windows PAC URL")
		}
	} else {
		options.flags = 1           // WINHTTP_AUTOPROXY_AUTO_DETECT
		options.autoDetectFlags = 3 // DHCP | DNS_A
	}

	// Register a single process callback; IDs prevent late callbacks from using
	// released Go objects or a subsequently reused Windows handle.
	previous, _, err := winHTTPSetCallback.Call(session, proxyCallback, 0x200000|0x1000000, 0)
	if previous == ^uintptr(0) {
		return "", fmt.Errorf("WinHttpSetStatusCallback: %w", err)
	}
	var resolver uintptr
	code, _, _ := winHTTPCreateResolver.Call(session, uintptr(unsafe.Pointer(&resolver)))
	if code != 0 {
		return "", syscall.Errno(code)
	}
	defer func() { _, _, _ = winHTTPCloseHandle.Call(resolver) }()
	id := uintptr(proxyCallbackID.Add(1))
	done := make(chan error, 1)
	proxyCallbacks.Store(id, done)
	defer proxyCallbacks.Delete(id)
	for {
		code, _, _ := winHTTPGetProxyForURL.Call(
			resolver,
			uintptr(unsafe.Pointer(address)),
			uintptr(unsafe.Pointer(&options)),
			id,
		)
		if code != 997 {
			return "", fmt.Errorf("WinHttpGetProxyForUrlEx: %w", syscall.Errno(code))
		}
		select {
		case err = <-done:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if errors.Is(err, syscall.Errno(12015)) && options.autoLogon == 0 {
			options.autoLogon = 1
			// A resolver handle serves one request, so the retry needs a new one.
			_, _, _ = winHTTPCloseHandle.Call(resolver)
			code, _, _ = winHTTPCreateResolver.Call(session, uintptr(unsafe.Pointer(&resolver)))
			if code != 0 {
				return "", syscall.Errno(code)
			}
			continue
		}
		if errors.Is(err, syscall.Errno(12180)) {
			return "", errNoAutoProxy
		}
		if err != nil {
			return "", fmt.Errorf("WinHttpGetProxyForUrlEx: %w", err)
		}
		break
	}
	var result winHTTPProxyResult
	code, _, _ = winHTTPGetResult.Call(resolver, uintptr(unsafe.Pointer(&result)))
	if code != 0 {
		return "", fmt.Errorf("WinHttpGetProxyResult: %w", syscall.Errno(code))
	}
	defer func() { _, _, _ = winHTTPFreeResult.Call(uintptr(unsafe.Pointer(&result))) }()

	// An empty list is NOT DIRECT: WinHTTP drops unsupported PAC directives,
	// including SOCKS5. Only an explicit non-proxy entry authorizes direct access.
	if result.count == 0 || result.entries == nil {
		return "", errUnsupportedPAC
	}
	var entries []string
	for _, entry := range unsafe.Slice(result.entries, int(result.count)) {
		if entry.proxy == 0 {
			entries = append(entries, "DIRECT")
			continue
		}
		scheme := ""
		switch entry.scheme {
		case 1:
			scheme = "http"
		case 2:
			scheme = "https"
		default:
			return "", errUnsupportedPAC
		}
		host := windows.UTF16PtrToString(entry.host)
		if host == "" || entry.port == 0 {
			return "", errors.New("invalid Windows PAC proxy endpoint")
		}
		entries = append(entries, scheme+"://"+net.JoinHostPort(host, strconv.Itoa(int(entry.port))))
	}
	return strings.Join(entries, ";"), nil
}
func freeWindowsProxyStrings(values ...*uint16) {
	for _, value := range values {
		if value != nil {
			_, _, _ = systemProxyGlobalFree.Call(uintptr(unsafe.Pointer(value)))
		}
	}
}
