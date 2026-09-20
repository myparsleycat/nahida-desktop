package gamebanana

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type passCrypto struct{}

func (passCrypto) EncryptString(value string) (string, error) { return value, nil }

func (passCrypto) DecryptString(value string) (string, error) { return value, nil }

func TestSetManualRMCTokenValidatesAndPersistsOnlyRMC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/apiv13/Member/Navigator/Personal" || request.Header.Get("Cookie") != "rmc=secret" {
			t.Fatalf("path = %q, cookie = %q", request.URL.Path, request.Header.Get("Cookie"))
		}
		w.Header().Add("Set-Cookie", "session=temporary; Path=/; HttpOnly")
		_, _ = io.WriteString(
			w,
			`{"_sUsername":"member","_sProfileUrl":"https://gamebanana.com/members/1","_sAvatarUrl":"avatar"}`,
		)
	}))
	defer server.Close()
	service, client := gameBananaTestService(t, server)
	result, err := service.SetManualRMCToken(context.Background(), " rmc=secret ")
	if err != nil || !result.OK {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=secret" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestSetManualRMCTokenMapsContextDeadlineToServerUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	result, err := service.SetManualRMCToken(ctx, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.ErrorCode != errCodeServerUnreachable {
		t.Fatalf("result = %+v, want %s", result, errCodeServerUnreachable)
	}
}

func TestGetGameOverviewFetchesComponentsConcurrently(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			maximum := maxInFlight.Load()
			if current <= maximum || maxInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		switch request.URL.Path {
		case "/apiv13/Game/8552/ProfilePage":
			_, _ = io.WriteString(
				w,
				`{"_idRow":8552,"_sName":"Game","_sProfileUrl":"https://gamebanana.com/games/8552","_aModRootCategories":[]}`,
			)
		case "/apiv13/Game/8552/TopSubs":
			_, _ = io.WriteString(w, `[]`)
		case "/apiv13/Game/8552/Subfeed":
			_, _ = io.WriteString(
				w,
				`{"_aMetadata":{"_nRecordCount":0,"_nPerpage":15,"_bIsComplete":true},"_aRecords":[]}`,
			)
		default:
			t.Errorf("unexpected URL %s", request.URL)
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	done := make(chan error, 1)
	go func() {
		_, err := service.GetGameOverview(context.Background(), 8552)
		done <- err
	}()
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("overview component requests did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if maxInFlight.Load() < 3 {
		t.Fatalf("max in-flight requests = %d, want 3", maxInFlight.Load())
	}
}

func TestToggleModLikeLogsStageAndContextOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	var output bytes.Buffer
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})

	if _, err := service.ToggleModLike(context.Background(), ModOverviewInput{ItemID: 42}); err == nil {
		t.Fatal("ToggleModLike succeeded with malformed profile")
	}
	logged := output.String()
	for _, want := range []string{"[GameBanana]", `"operation":"toggle-mod-like"`, "profile-fetch", "cacheState", "cleanupState", `"itemId":42`, `"endpoint":"/Mod/42/ProfilePage"`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %q does not contain %q", logged, want)
		}
	}
	if strings.Count(logged, "GAMEBANANA_SCHEMA_ERROR") != 1 {
		t.Fatalf("failure was not logged exactly once: %q", logged)
	}
}

func TestGameBananaRequestsUseBrowserUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != gameBananaUserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, gameBananaUserAgent)
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)

	valid, _, err := service.validateCandidateRMCCookie(context.Background(), "rmc=secret")
	if err != nil || !valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}
}

func TestGetGameSubfeedAppliesDefaultsAndGamesMatchSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/apiv13/Game/8552/Subfeed" || request.URL.Query().Get("_sSort") != "default" ||
			request.URL.Query().Get("_nPage") != "1" {
			t.Fatalf("URL = %s", request.URL)
		}
		_, _ = io.WriteString(w, `{"_aMetadata":{"_nRecordCount":0,"_nPerpage":15,"_bIsComplete":true},"_aRecords":[]}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if _, err := service.GetGameSubfeed(context.Background(), GameSubfeedInput{GameID: 8552}); err != nil {
		t.Fatal(err)
	}
	if service.GetGames()["nte"] != 23012 {
		t.Fatalf("games = %v", service.GetGames())
	}
}

func TestResponseSchemasRejectMalformedPayloads(t *testing.T) {
	const (
		validGameProfile = `{"_idRow":8552,"_sName":"Game","_sProfileUrl":"https://gamebanana.com/games/8552","_aModRootCategories":[]}`
		validModIndex    = `{"_aMetadata":{"_nRecordCount":0,"_nPerpage":15,"_bIsComplete":true},"_aRecords":[]}`
		validModProfile  = `{"_idRow":10,"_sName":"Mod","_sProfileUrl":"https://gamebanana.com/mods/10","_aSubmitter":{"_sName":"author"},"_aGame":{"_idRow":8552,"_sName":"Game"},"_aCategory":{"_sName":"Category"}}`
	)
	tests := []struct {
		name      string
		context   string
		responses map[string]string
		invoke    func(*GameBanana) error
	}{
		{
			name:    "game profile",
			context: "game_profile",
			responses: map[string]string{
				"/apiv13/Game/8552/ProfilePage": `{}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetGameOverview(context.Background(), 8552)
				return err
			},
		},
		{
			name:    "game top submissions",
			context: "game_top_submissions",
			responses: map[string]string{
				"/apiv13/Game/8552/ProfilePage": validGameProfile,
				"/apiv13/Game/8552/TopSubs":     `{}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetGameOverview(context.Background(), 8552)
				return err
			},
		},
		{
			name:    "game subfeed",
			context: "game_subfeed",
			responses: map[string]string{
				"/apiv13/Game/8552/Subfeed": `{"_aMetadata":{},"_aRecords":[{}]}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetGameSubfeed(context.Background(), GameSubfeedInput{GameID: 8552})
				return err
			},
		},
		{
			name:    "mod index",
			context: "mod_index",
			responses: map[string]string{
				"/apiv13/Mod/Index": `{"_aMetadata":{},"_aRecords":[{}]}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetModIndex(context.Background(), ModIndexInput{CategoryID: 7})
				return err
			},
		},
		{
			name:    "mod category profile",
			context: "mod_category_profile",
			responses: map[string]string{
				"/apiv13/ModCategory/7/ProfilePage": `{}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetModCategoryOverview(context.Background(), ModCategoryOverviewInput{CategoryID: 7})
				return err
			},
		},
		{
			name:    "mod categories",
			context: "mod_categories",
			responses: map[string]string{
				"/apiv13/ModCategory/7/ProfilePage": `{"_idRow":7,"_sName":"Category"}`,
				"/apiv13/Mod/Index":                 validModIndex,
				"/apiv13/Mod/Categories":            `[{}]`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetModCategoryOverview(context.Background(), ModCategoryOverviewInput{CategoryID: 7})
				return err
			},
		},
		{
			name:    "mod profile",
			context: "mod_profile",
			responses: map[string]string{
				"/apiv13/Mod/10/ProfilePage": `{}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetModOverview(context.Background(), ModOverviewInput{ItemID: 10})
				return err
			},
		},
		{
			name:    "mod config",
			context: "mod_config",
			responses: map[string]string{
				"/apiv13/Mod/10/ProfilePage": validModProfile,
				"/apiv13/Mod/10/Config":      `{"_aAccess":{"Like_Trash":"yes"}}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetModOverview(context.Background(), ModOverviewInput{ItemID: 10})
				return err
			},
		},
		{
			name:    "mod posts",
			context: "mod_posts",
			responses: map[string]string{
				"/apiv13/Mod/10/Posts": `{"_aMetadata":{},"_aRecords":[{}]}`,
			},
			invoke: func(service *GameBanana) error {
				_, err := service.GetModPosts(context.Background(), ModPostsInput{ModID: 10})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body, ok := test.responses[request.URL.Path]
				if !ok {
					http.NotFound(w, request)
					return
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			service, _ := gameBananaTestService(t, server)

			err := test.invoke(service)
			prefix := "GAMEBANANA_SCHEMA_ERROR:" + test.context + ":"
			if err == nil || !strings.HasPrefix(err.Error(), prefix) {
				t.Fatalf("error = %v, want prefix %q", err, prefix)
			}
			details := strings.TrimPrefix(err.Error(), prefix)
			if details == "" || strings.Count(details, " | ") > 2 {
				t.Fatalf("schema issue details = %q, want 1 to 3 issues", details)
			}
		})
	}
}

func TestToggleModLikeUsesCachedProfileState(t *testing.T) {
	var requestsMu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestsMu.Lock()
		requests = append(requests, request.Method+" "+request.URL.Path)
		requestsMu.Unlock()
		switch request.URL.Path {
		case "/apiv13/Mod/10/ProfilePage":
			_, _ = io.WriteString(
				w,
				`{"_idRow":10,"_sName":"Mod","_sProfileUrl":"https://gamebanana.com/mods/10","_bAccessorHasLiked":true,"_aSubmitter":{"_sName":"author"},"_aGame":{"_idRow":8552,"_sName":"Game"},"_aCategory":{"_sName":"Category"}}`,
			)
		case "/apiv13/Mod/10/Config":
			_, _ = io.WriteString(w, `{}`)
		case "/apiv13/Mod/10/Like":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if _, err := service.GetModOverview(context.Background(), ModOverviewInput{ItemID: 10}); err != nil {
		t.Fatal(err)
	}
	result, err := service.ToggleModLike(context.Background(), ModOverviewInput{ItemID: 10})
	if err != nil {
		t.Fatal(err)
	}
	requestsMu.Lock()
	requests = append([]string(nil), requests...)
	requestsMu.Unlock()
	if result.Liked || len(requests) != 3 || requests[2] != "DELETE /apiv13/Mod/10/Like" {
		t.Fatalf("result = %+v, requests = %v", result, requests)
	}
}

func TestToggleModLikeRequiresLoginWithoutOpeningWindow(t *testing.T) {
	var likeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apiv13/Mod/10/ProfilePage":
			_, _ = io.WriteString(
				w,
				`{"_idRow":10,"_sName":"Mod","_sProfileUrl":"https://gamebanana.com/mods/10","_bAccessorHasLiked":false,"_aSubmitter":{"_sName":"author"},"_aGame":{"_idRow":8552,"_sName":"Game"},"_aCategory":{"_sName":"Category"}}`,
			)
		case "/apiv13/Mod/10/Config":
			_, _ = io.WriteString(w, `{}`)
		case "/apiv13/Mod/10/Like":
			likeCalls++
			if request.Method != http.MethodPost {
				t.Fatalf("method = %s", request.Method)
			}
			_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	var opens atomic.Int32
	service.openLogin = func(context.Context, CookieValidator) (string, error) {
		opens.Add(1)
		return "", ErrLoginCancelled
	}
	_, err := service.ToggleModLike(context.Background(), ModOverviewInput{ItemID: 10})
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("error = %v, want %s", err, ErrAuthRequired)
	}
	if likeCalls != 1 {
		t.Fatalf("like calls = %d, want 1", likeCalls)
	}
	if opens.Load() != 0 {
		t.Fatalf("login window opened %d times, want 0", opens.Load())
	}
}

func TestRequestDropsRejectedSessionAndRetriesAnonymous(t *testing.T) {
	var subfeedCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/apiv13/Game/8552/Subfeed" {
			http.NotFound(w, request)
			return
		}
		subfeedCalls++
		if request.Header.Get("Cookie") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(
			w,
			`{"_aMetadata":{"_nRecordCount":0,"_nPerpage":15,"_bIsComplete":true},"_aRecords":[]}`,
		)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=expired"); err != nil {
		t.Fatal(err)
	}
	var opens atomic.Int32
	service.openLogin = func(context.Context, CookieValidator) (string, error) {
		opens.Add(1)
		return "", ErrLoginCancelled
	}
	if _, err := service.GetGameSubfeed(context.Background(), GameSubfeedInput{GameID: 8552}); err != nil {
		t.Fatal(err)
	}
	if subfeedCalls != 2 {
		t.Fatalf("subfeed calls = %d, want 2", subfeedCalls)
	}
	if opens.Load() != 0 {
		t.Fatalf("login window opened %d times, want 0", opens.Load())
	}
	cookie, err := service.getCookie(context.Background())
	if err != nil || cookie != "" {
		t.Fatalf("rejected session = %q, error = %v", cookie, err)
	}
}

func TestRequestReportsAuthRequiredWhenAnonymousRetryFails(t *testing.T) {
	var subfeedCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/apiv13/Game/8552/Subfeed" {
			http.NotFound(w, request)
			return
		}
		subfeedCalls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=expired"); err != nil {
		t.Fatal(err)
	}
	var opens atomic.Int32
	service.openLogin = func(context.Context, CookieValidator) (string, error) {
		opens.Add(1)
		return "", ErrLoginCancelled
	}
	_, err := service.GetGameSubfeed(context.Background(), GameSubfeedInput{GameID: 8552})
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("error = %v, want %s", err, ErrAuthRequired)
	}
	if subfeedCalls != 2 {
		t.Fatalf("subfeed calls = %d, want 2", subfeedCalls)
	}
	if opens.Load() != 0 {
		t.Fatalf("login window opened %d times, want 0", opens.Load())
	}
}

func TestConcurrentRequestRetriesAnonymousAfterSiblingDropsSession(t *testing.T) {
	var siblingCalls atomic.Int32
	siblingStarted := make(chan struct{})
	sessionDropped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/apiv13/sibling" && siblingCalls.Add(1) == 1 {
			// Hold the rejection until the sibling request has dropped the session,
			// so this request reaches its anonymous retry with the cookie already gone.
			close(siblingStarted)
			<-sessionDropped
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
			return
		}
		if request.Header.Get("Cookie") == "" {
			if request.URL.Path == "/apiv13/owner" {
				close(sessionDropped)
			}
			_, _ = io.WriteString(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.saveCookie(ctx, "rmc=expired"); err != nil {
		t.Fatal(err)
	}
	policy := requestPolicy{
		PersistResponseCookies:  true,
		ClearStoredCookieOnAuth: true,
		AuthFallback:            authFallbackAnonymous,
	}

	sibling := make(chan error, 1)
	go func() {
		response, err := service.request(ctx, http.MethodGet, server.URL+"/apiv13/sibling", nil, policy)
		if response != nil {
			_ = response.Body.Close()
		}
		sibling <- err
	}()
	<-siblingStarted

	response, err := service.request(ctx, http.MethodGet, server.URL+"/apiv13/owner", nil, policy)
	if response != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatalf("session-dropping request = %v", err)
	}
	if err := <-sibling; err != nil {
		t.Fatalf("sibling request = %v, want the anonymous retry to succeed", err)
	}
	if calls := siblingCalls.Load(); calls != 2 {
		t.Fatalf("sibling calls = %d, want 2", calls)
	}
	cookie, err := service.getCookie(ctx)
	if err != nil || cookie != "" {
		t.Fatalf("stored session = %q, error = %v", cookie, err)
	}
}

func TestAnonymousAuthRequiredResponseKeepsStoredSessionWritable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	ctx := context.Background()
	_, revision, err := service.cookieSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.request(ctx, http.MethodGet, server.URL+"/apiv13/anonymous", nil, requestPolicy{
		PersistResponseCookies:  true,
		ClearStoredCookieOnAuth: true,
		AuthFallback:            authFallbackAnonymous,
	})
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("error = %v, want %s", err, ErrAuthRequired)
	}

	applied, err := service.updateCookie(ctx, revision, "rmc=accepted")
	if err != nil || !applied {
		t.Fatalf("session accepted during the anonymous request = %v, %v", applied, err)
	}
}

func TestNormalizeRMCCookieKeepsElectronFallbackWithoutRMCSegment(t *testing.T) {
	if cookie, err := normalizeRMCCookie("secret; other=value"); err != nil || cookie != "rmc=secret; other=value" {
		t.Fatalf("cookie = %q, err = %v", cookie, err)
	}
}

func TestGetGameSubfeedNormalizesSingleScreenshotPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(w, `{"_aMetadata":{"_nRecordCount":1,"_nPerpage":15,"_bIsComplete":true},"_aRecords":[{
			"_idRow":710296,
			"_sModelName":"Mod",
			"_sName":"mod",
			"_aSubmitter":{"_sName":"author"},
			"_aPreviewContent":{"screenshot":{"_sBaseUrl":"https://images.gamebanana.com/img/ss/mods","_sFile":"abc.jpg","_sFile530":"abc_530.webp"}}
		}]}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	result, err := service.GetGameSubfeed(context.Background(), GameSubfeedInput{GameID: 8552})
	if err != nil {
		t.Fatal(err)
	}
	feed, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result = %T", result)
	}
	records, _ := feed["_aRecords"].([]any)
	record, _ := records[0].(map[string]any)
	previewContent, _ := record["_aPreviewContent"].(map[string]any)
	screenshots, _ := previewContent["screenshots"].([]any)
	preview, _ := screenshots[0].(map[string]any)
	if preview == nil || preview["_sFile530"] != "abc_530.webp" {
		t.Fatalf("preview = %v", previewContent)
	}
}

func TestGetModOverviewNormalizesPreviewShapes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apiv13/Mod/10/ProfilePage":
			_, _ = io.WriteString(
				w,
				`{"_idRow":10,"_sName":"Mod","_sProfileUrl":"https://gamebanana.com/mods/10","_aSubmitter":{"_sName":"author"},"_aGame":{"_idRow":8552,"_sName":"Game"},"_aCategory":{"_sName":"Category"},"_aPreviewContent":{"screenshot":{"_sBaseUrl":"https://images.gamebanana.com/img/ss/mods","_sFile":"abc.jpg"}},"_aFiles":[{"_idRow":20,"_sDownloadUrl":"https://gamebanana.com/dl/20","_sFile":"mod.zip","_tsDateAdded":1,"_nDownloadCount":1}]}`,
			)
		case "/apiv13/Mod/10/Config":
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	overview, err := service.GetModOverview(context.Background(), ModOverviewInput{ItemID: 10})
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := overview["profile"].(map[string]any)
	previewContent, _ := profile["_aPreviewContent"].(map[string]any)
	screenshots, _ := previewContent["screenshots"].([]any)
	if len(screenshots) != 1 || screenshots[0].(map[string]any)["_sFile"] != "abc.jpg" {
		t.Fatalf("preview = %v", previewContent)
	}
	payload, err := service.GetDownloadFilePayload(context.Background(), DownloadFileInput{ItemID: 10, FileID: 20})
	if err != nil {
		t.Fatal(err)
	}
	if payload.PreviewURL == nil || *payload.PreviewURL != "https://images.gamebanana.com/img/ss/mods/abc.jpg" {
		t.Fatalf("preview URL = %v", payload.PreviewURL)
	}
}

func TestNormalizePreviewMediaKeepsExistingScreenshotsArray(t *testing.T) {
	value := map[string]any{"screenshots": []any{map[string]any{"_sFile": "a.jpg"}}}
	normalized, ok := normalizePreviewMedia(value).(map[string]any)
	if !ok || len(normalized["screenshots"].([]any)) != 1 {
		t.Fatalf("normalized = %v", normalized)
	}
	if normalized := normalizePreviewMedia([]any{}); len(normalized.(map[string]any)["screenshots"].([]any)) != 0 {
		t.Fatalf("normalized = %v", normalized)
	}
	if normalized := normalizePreviewMedia(map[string]any{"_sFile": "a.jpg"}); len(normalized.(map[string]any)) != 1 {
		t.Fatalf("normalized = %v", normalized)
	}
}

func gameBananaTestService(t *testing.T, server *httptest.Server) (*GameBanana, *db.Client) {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	httpClient := infra.NewClientWithOptions(
		infra.ClientOptions{HTTPClient: server.Client(), Status: infra.BackendOnline},
	)
	service := NewWithOptions(
		Options{HTTP: httpClient, Crypto: passCrypto{}, BaseURL: server.URL + "/apiv13", SiteURL: server.URL},
	)
	service.UseClient(client)
	return service, client
}
