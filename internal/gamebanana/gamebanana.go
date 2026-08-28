package gamebanana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

const (
	cookieSettingKey    = "gamebanana_auth_cookies"
	gameBananaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
)

var submissionModelPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

var games = map[string]int{
	"gi":  8552,
	"sr":  18366,
	"hi":  10349,
	"zz":  19567,
	"ww":  20357,
	"ef":  21842,
	"nte": 23012,
}

type Crypto interface {
	EncryptString(string) (string, error)
	DecryptString(string) (string, error)
}

type Options struct {
	HTTP              *infra.Client
	Log               *infra.Log
	Crypto            Crypto
	BaseURL           string
	SiteURL           string
	OpenLogin         OpenLoginFunc
	ClearLoginCookies ClearLoginCookiesFunc
}

type ManualRMCSaveResult struct {
	OK        bool   `json:"ok"`
	ErrorCode string `json:"errorCode,omitempty"`
}

type GameSubfeedInput struct {
	GameID int    `json:"gameId"`
	Sort   string `json:"sort,omitempty"`
	Page   int    `json:"page,omitempty"`
}

type ModIndexInput struct {
	CategoryID int    `json:"categoryId"`
	PerPage    int    `json:"perPage,omitempty"`
	Page       int    `json:"page,omitempty"`
	Sort       string `json:"sort,omitempty"`
}

type ModCategoryOverviewInput struct {
	CategoryID int    `json:"categoryId"`
	PerPage    int    `json:"perPage,omitempty"`
	Page       int    `json:"page,omitempty"`
	Sort       string `json:"sort,omitempty"`
	ShowEmpty  *bool  `json:"showEmpty,omitempty"`
	ModSort    string `json:"modSort,omitempty"`
}

type ModOverviewInput struct {
	ItemID    int    `json:"itemId"`
	ModelName string `json:"modelName,omitempty"`
}

type ModPostsInput struct {
	ModID     int    `json:"modId"`
	ModelName string `json:"modelName,omitempty"`
	Page      int    `json:"page,omitempty"`
	PerPage   int    `json:"perPage,omitempty"`
	Sort      string `json:"sort,omitempty"`
}

type ToggleLikeResult struct {
	Liked bool `json:"liked"`
}

type GameBanana struct {
	mu             sync.Mutex
	http           *infra.Client
	log            *infra.Log
	crypto         Crypto
	client         *db.Client
	baseURL        string
	siteURL        string
	sessionCookie  string
	lastProfileID  int
	lastModelName  string
	lastModProfile map[string]any

	loginMu           sync.Mutex
	login             *loginCall
	openLogin         OpenLoginFunc
	clearLoginCookies ClearLoginCookiesFunc
}

func New() *GameBanana {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *GameBanana {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://gamebanana.com/apiv13"
	}
	siteURL := strings.TrimRight(opts.SiteURL, "/")
	if siteURL == "" {
		siteURL = "https://gamebanana.com"
	}
	return &GameBanana{
		http:              opts.HTTP,
		log:               opts.Log,
		crypto:            opts.Crypto,
		baseURL:           baseURL,
		siteURL:           siteURL,
		openLogin:         opts.OpenLogin,
		clearLoginCookies: opts.ClearLoginCookies,
	}
}

//wails:ignore
func (g *GameBanana) UseClient(client *db.Client) {
	g.mu.Lock()
	g.client = client
	g.mu.Unlock()
}

func (g *GameBanana) GetGames() map[string]int {
	out := make(map[string]int, len(games))
	for key, value := range games {
		out[key] = value
	}
	return out
}

func (g *GameBanana) EnsureSession(ctx context.Context) error {
	cookie, err := g.getCookie(ctx)
	if err != nil {
		return err
	}

	if cookie != "" {
		valid, _, err := g.validateStoredRMCCookie(ctx, cookie)
		if err != nil {
			return classifyLoginError(err)
		}
		if valid {
			return nil
		}
		if err := g.removeCookie(ctx); err != nil {
			return err
		}
	}

	_, err = g.openAuthenticatedSession(ctx)
	return err
}

func (g *GameBanana) SetManualRMCToken(ctx context.Context, input string) (ManualRMCSaveResult, error) {
	cookie, normalized := normalizedRMCCookie(input)
	if !normalized {
		return ManualRMCSaveResult{ErrorCode: "GAMEBANANA_INVALID_RMC"}, nil
	}
	valid, merged, err := g.validateCandidateRMCCookie(ctx, cookie)
	if err != nil {
		code := "GAMEBANANA_MANUAL_RMC_SAVE_FAILED"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(classifyLoginError(err), ErrServerUnreachable) {
			code = errCodeServerUnreachable
		}
		return ManualRMCSaveResult{ErrorCode: code}, nil
	}
	if !valid {
		return ManualRMCSaveResult{ErrorCode: "GAMEBANANA_INVALID_RMC"}, nil
	}
	if merged == "" {
		merged = cookie
	}
	if !g.persistManualCookie(ctx, merged) {
		return ManualRMCSaveResult{ErrorCode: "GAMEBANANA_MANUAL_RMC_SAVE_FAILED"}, nil
	}
	return ManualRMCSaveResult{OK: true}, nil
}

func (g *GameBanana) Logout(ctx context.Context) error {
	g.cancelLogin()
	cookie, err := g.getCookie(ctx)
	if err != nil {
		return err
	}
	if cookie != "" {
		response, requestErr := g.request(ctx, http.MethodGet, g.siteURL+"/members/account/logout", nil, requestPolicy{
			Cookie:                  cookie,
			PersistResponseCookies:  false,
			ClearStoredCookieOnAuth: false,
			SkipAuthRetry:           true,
		})
		if response != nil {
			_ = response.Body.Close()
		}
		if requestErr != nil && g.log != nil {
			g.log.Warn(sanitizeLogMessage(requestErr.Error()), "GameBananaService.logout")
		}
	}
	if err := g.removeCookie(ctx); err != nil {
		return err
	}
	if g.clearLoginCookies != nil {
		if clearErr := g.clearLoginCookies(ctx); clearErr != nil {
			return clearErr
		}
	}
	return nil
}

func (g *GameBanana) GetGameOverview(ctx context.Context, gameID int) (map[string]any, error) {
	var profile, top, subfeed any
	var profileErr, topErr, subfeedErr error
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		profile, profileErr = g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/Game/%d/ProfilePage", gameID), nil, gameReferer(gameID), gameProfileResponseSchema)
	}()
	go func() {
		defer wait.Done()
		top, topErr = g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/Game/%d/TopSubs", gameID), nil, gameReferer(gameID), gameTopSubsResponseSchema)
	}()
	go func() {
		defer wait.Done()
		subfeed, subfeedErr = g.GetGameSubfeed(ctx, GameSubfeedInput{GameID: gameID})
	}()
	wait.Wait()
	if profileErr != nil {
		return nil, profileErr
	}
	if topErr != nil {
		return nil, topErr
	}
	if subfeedErr != nil {
		return nil, subfeedErr
	}
	return map[string]any{"profile": profile, "topSubs": top, "subfeed": subfeed}, nil
}

func (g *GameBanana) GetGameSubfeed(ctx context.Context, input GameSubfeedInput) (any, error) {
	if input.Sort == "" {
		input.Sort = "default"
	}
	if input.Page < 1 {
		input.Page = 1
	}
	query := url.Values{"_sSort": []string{input.Sort}, "_nPage": []string{strconv.Itoa(input.Page)}}
	return g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/Game/%d/Subfeed", input.GameID), query, gameReferer(input.GameID), gameSubfeedResponseSchema)
}

func (g *GameBanana) GetModIndex(ctx context.Context, input ModIndexInput) (any, error) {
	applyModIndexDefaults(&input)
	query := url.Values{
		"_nPerpage":                   []string{strconv.Itoa(input.PerPage)},
		"_aFilters[Generic_Category]": []string{strconv.Itoa(input.CategoryID)},
		"_nPage":                      []string{strconv.Itoa(input.Page)},
		"_sSort":                      []string{input.Sort},
	}
	return g.getJSON(ctx, http.MethodGet, "/Mod/Index", query, categoryReferer(input.CategoryID), modIndexResponseSchema)
}

func (g *GameBanana) GetModCategoryOverview(ctx context.Context, input ModCategoryOverviewInput) (map[string]any, error) {
	indexInput := ModIndexInput{CategoryID: input.CategoryID, PerPage: input.PerPage, Page: input.Page, Sort: input.ModSort}
	applyModIndexDefaults(&indexInput)
	if input.Sort == "" {
		input.Sort = "a_to_z"
	}
	showEmpty := true
	if input.ShowEmpty != nil {
		showEmpty = *input.ShowEmpty
	}
	query := url.Values{"_idCategoryRow": []string{strconv.Itoa(input.CategoryID)}, "_sSort": []string{input.Sort}, "_bShowEmpty": []string{strconv.FormatBool(showEmpty)}}
	var profile, index, categories any
	var profileErr, indexErr, categoriesErr error
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		profile, profileErr = g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/ModCategory/%d/ProfilePage", input.CategoryID), nil, categoryReferer(input.CategoryID), modCategoryProfileResponseSchema)
	}()
	go func() {
		defer wait.Done()
		index, indexErr = g.GetModIndex(ctx, indexInput)
	}()
	go func() {
		defer wait.Done()
		categories, categoriesErr = g.getJSON(ctx, http.MethodGet, "/Mod/Categories", query, categoryReferer(input.CategoryID), modCategoriesResponseSchema)
	}()
	wait.Wait()
	if profileErr != nil {
		return nil, profileErr
	}
	if indexErr != nil {
		return nil, indexErr
	}
	if categoriesErr != nil {
		return nil, categoriesErr
	}
	return map[string]any{"profile": profile, "index": index, "categories": categories}, nil
}

func (g *GameBanana) GetModOverview(ctx context.Context, input ModOverviewInput) (map[string]any, error) {
	model, err := normalizeModelName(input.ModelName)
	if err != nil {
		return nil, err
	}
	referer := submissionReferer(model, input.ItemID)
	var profileValue, config any
	var profileErr, configErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		profileValue, profileErr = g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/%s/%d/ProfilePage", model, input.ItemID), nil, referer, modelResponseSchema(model, "profile", modProfileSchema))
	}()
	go func() {
		defer wait.Done()
		config, configErr = g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/%s/%d/Config", model, input.ItemID), nil, referer, modelResponseSchema(model, "config", modConfigSchema))
	}()
	wait.Wait()
	if profileErr != nil {
		return nil, profileErr
	}
	profile, ok := profileValue.(map[string]any)
	if !ok {
		return nil, errors.New("GAMEBANANA_SCHEMA_ERROR:mod_profile")
	}
	g.mu.Lock()
	g.lastProfileID = input.ItemID
	g.lastModelName = model
	g.lastModProfile = cloneRecord(profile)
	g.mu.Unlock()
	if configErr != nil {
		return nil, configErr
	}
	return map[string]any{"profile": profile, "config": config}, nil
}

func (g *GameBanana) ToggleModLike(ctx context.Context, input ModOverviewInput) (result ToggleLikeResult, err error) {
	stage, cacheState, cleanupState := "profile-cache", "miss", "not-started"
	modelName := input.ModelName
	if modelName == "" {
		modelName = "Mod"
	}
	defer func() {
		if err == nil || g.log == nil {
			return
		}
		g.log.Error(sanitizeLogMessage(err.Error()), "GameBanana:toggleModLike")
		g.log.Error(map[string]any{
			"channel": "gamebanana:toggleModLike", "operation": "toggleModLike",
			"itemId": input.ItemID, "modelName": modelName,
			"stage": stage, "cacheState": cacheState, "cleanupState": cleanupState,
			"error": sanitizeLogMessage(err.Error()),
		}, "GameBanana:toggleModLike:context")
	}()
	model, err := normalizeModelName(input.ModelName)
	if err != nil {
		return ToggleLikeResult{}, err
	}
	g.mu.Lock()
	profile := map[string]any(nil)
	if g.lastProfileID == input.ItemID && g.lastModelName == model {
		profile = cloneRecord(g.lastModProfile)
	}
	g.mu.Unlock()
	if profile != nil {
		cacheState = "hit"
	}
	if profile == nil {
		stage = "profile-fetch"
		overview, getErr := g.GetModOverview(ctx, ModOverviewInput{ItemID: input.ItemID, ModelName: model})
		if getErr != nil {
			return ToggleLikeResult{}, getErr
		}
		profile, _ = overview["profile"].(map[string]any)
	}
	wasLiked, known := profile["_bAccessorHasLiked"].(bool)
	if !known {
		stage = "config-fetch"
		config, getErr := g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/%s/%d/Config", model, input.ItemID), nil, submissionReferer(model, input.ItemID), modelResponseSchema(model, "config", modConfigSchema))
		if getErr != nil {
			return ToggleLikeResult{}, getErr
		}
		if record, ok := config.(map[string]any); ok {
			if access, accessOK := record["_aAccess"].(map[string]any); accessOK {
				wasLiked, _ = access["Like_Trash"].(bool)
			}
		}
	}
	method := http.MethodPost
	if wasLiked {
		method = http.MethodDelete
	}
	stage = "like-mutation"
	response, err := g.request(ctx, method, g.baseURL+fmt.Sprintf("/%s/%d/Like", model, input.ItemID), http.Header{"Referer": []string{submissionReferer(model, input.ItemID)}}, requestPolicy{
		PersistResponseCookies:  true,
		ClearStoredCookieOnAuth: true,
	})
	if err != nil {
		return ToggleLikeResult{}, err
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return ToggleLikeResult{}, err
	}
	if isLoginRequiredBody(body) {
		return ToggleLikeResult{}, ErrAuthRequired
	}
	g.mu.Lock()
	g.lastModProfile = nil
	g.lastProfileID = 0
	g.lastModelName = ""
	g.mu.Unlock()
	cleanupState = "profile-cache-cleared"
	return ToggleLikeResult{Liked: !wasLiked}, nil
}

func (g *GameBanana) GetModPosts(ctx context.Context, input ModPostsInput) (any, error) {
	model, err := normalizeModelName(input.ModelName)
	if err != nil {
		return nil, err
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PerPage < 1 {
		input.PerPage = 15
	}
	if input.Sort == "" {
		input.Sort = "popular"
	}
	query := url.Values{"_nPage": []string{strconv.Itoa(input.Page)}, "_nPerpage": []string{strconv.Itoa(input.PerPage)}, "_sSort": []string{input.Sort}}
	return g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/%s/%d/Posts", model, input.ModID), query, submissionReferer(model, input.ModID), modelResponseSchema(model, "posts", modPostsSchema))
}

func (g *GameBanana) getJSON(ctx context.Context, method, path string, query url.Values, referer string, schema responseSchema) (any, error) {
	rawURL := g.baseURL + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	header := make(http.Header)
	if referer != "" {
		header.Set("Referer", referer)
	}
	response, err := g.request(ctx, method, rawURL, header, requestPolicy{
		PersistResponseCookies:  true,
		ClearStoredCookieOnAuth: true,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	var value any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return nil, err
	}
	if record, ok := value.(map[string]any); ok && record["_sErrorCode"] == "LOGIN_REQUIRED" {
		return nil, ErrAuthRequired
	}
	value = normalizePreviewContent(value)
	if err := schema.validate(value); err != nil {
		if g.log != nil {
			g.log.Error(err.Error(), "GameBananaService:requestJson")
		}
		return nil, err
	}
	return value, nil
}

func isLoginRequiredBody(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var record map[string]any
	if err := json.Unmarshal(body, &record); err != nil {
		return false
	}
	return record["_sErrorCode"] == "LOGIN_REQUIRED"
}

type gameBananaHTTPError struct {
	Status int
}

func (e *gameBananaHTTPError) Error() string {
	return fmt.Sprintf("GAMEBANANA_HTTP_ERROR:%d:%s", e.Status, http.StatusText(e.Status))
}

type requestPolicy struct {
	Cookie                  string
	PersistResponseCookies  bool
	ClearStoredCookieOnAuth bool
	SkipAuthRetry           bool
}

func (g *GameBanana) request(ctx context.Context, method, rawURL string, header http.Header, policy requestPolicy) (*http.Response, error) {
	if g.http == nil {
		return nil, errors.New("GameBanana HTTP client is not configured")
	}
	if header == nil {
		header = make(http.Header)
	} else {
		header = header.Clone()
	}
	cookie := policy.Cookie
	if cookie == "" {
		var err error
		cookie, err = g.getCookie(ctx)
		if err != nil {
			return nil, err
		}
	}
	if cookie != "" {
		header.Set("Cookie", cookie)
	}
	header.Set("User-Agent", gameBananaUserAgent)
	response, err := g.http.Fetch(ctx, rawURL, infra.FetchOptions{Method: method, Header: header, DisableHTTPErrors: true})
	if err != nil {
		return nil, err
	}
	mergedCookie := cookie
	if policy.PersistResponseCookies {
		if merged := mergeSetCookies(cookie, response.Header.Values("Set-Cookie")); merged != "" && merged != cookie {
			if saveErr := g.saveCookie(ctx, merged); saveErr != nil {
				_ = response.Body.Close()
				return nil, saveErr
			}
			mergedCookie = merged
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			if !policy.SkipAuthRetry {
				return g.retryAuthenticatedRequest(ctx, method, rawURL, header, policy, cookie, mergedCookie)
			}
			if policy.ClearStoredCookieOnAuth {
				_ = g.removeCookie(ctx)
			}
			return nil, ErrAuthFailed
		}
		return nil, &gameBananaHTTPError{Status: response.StatusCode}
	}
	if body, readErr := io.ReadAll(response.Body); readErr != nil {
		_ = response.Body.Close()
		return nil, readErr
	} else {
		_ = response.Body.Close()
		response.Body = io.NopCloser(bytes.NewReader(body))
		if isLoginRequiredBody(body) {
			_ = response.Body.Close()
			if !policy.SkipAuthRetry {
				return g.retryAuthenticatedRequest(ctx, method, rawURL, header, policy, cookie, mergedCookie)
			}
			if policy.ClearStoredCookieOnAuth {
				_ = g.removeCookie(ctx)
			}
			return nil, ErrAuthFailed
		}
	}
	return response, nil
}

func (g *GameBanana) retryAuthenticatedRequest(
	ctx context.Context,
	method, rawURL string,
	header http.Header,
	policy requestPolicy,
	originalCookie, mergedCookie string,
) (*http.Response, error) {
	policy.SkipAuthRetry = true
	policy.Cookie = ""
	header = header.Clone()
	header.Del("Cookie")
	if mergedCookie == "" || mergedCookie == originalCookie {
		if err := g.EnsureSession(ctx); err != nil {
			return nil, err
		}
	}
	return g.request(ctx, method, rawURL, header, policy)
}

func (g *GameBanana) validateStoredRMCCookie(ctx context.Context, cookie string) (bool, string, error) {
	return g.validateRMCCookie(ctx, cookie, requestPolicy{
		Cookie:                  cookie,
		PersistResponseCookies:  false,
		ClearStoredCookieOnAuth: false,
		SkipAuthRetry:           true,
	})
}

func (g *GameBanana) validateCandidateRMCCookie(ctx context.Context, cookie string) (bool, string, error) {
	return g.validateRMCCookie(ctx, cookie, requestPolicy{
		Cookie:                  cookie,
		PersistResponseCookies:  false,
		ClearStoredCookieOnAuth: false,
		SkipAuthRetry:           true,
	})
}

func (g *GameBanana) validateRMCCookie(ctx context.Context, cookie string, policy requestPolicy) (bool, string, error) {
	if policy.Cookie == "" {
		policy.Cookie = cookie
	}
	response, err := g.request(ctx, http.MethodGet, g.baseURL+"/Member/Navigator/Personal", http.Header{"Cookie": []string{cookie}}, policy)
	if err != nil {
		if errors.Is(err, ErrAuthFailed) {
			return false, "", nil
		}
		return false, "", err
	}
	defer func() { _ = response.Body.Close() }()
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return false, "", nil
	}
	if value["_sErrorCode"] == "LOGIN_REQUIRED" {
		return false, "", nil
	}
	_, usernameOK := value["_sUsername"].(string)
	_, profileOK := value["_sProfileUrl"].(string)
	if !usernameOK || !profileOK {
		return false, "", nil
	}
	return true, mergeSetCookies(cookie, response.Header.Values("Set-Cookie")), nil
}

func (g *GameBanana) getCookie(ctx context.Context) (string, error) {
	g.mu.Lock()
	if g.sessionCookie != "" {
		cookie := g.sessionCookie
		g.mu.Unlock()
		return cookie, nil
	}
	client := g.client
	crypto := g.crypto
	g.mu.Unlock()
	if client == nil || crypto == nil {
		return "", nil
	}
	value, err := client.Settings.GetValue(ctx, cookieSettingKey)
	if err != nil || value == nil || *value == "" {
		return "", err
	}
	decrypted, decryptedOK := decryptStoredCookie(crypto, *value)
	if !decryptedOK {
		_ = g.removeCookie(ctx)
		return "", nil
	}
	g.mu.Lock()
	g.sessionCookie = decrypted
	g.mu.Unlock()
	return decrypted, nil
}

func decryptStoredCookie(crypto Crypto, value string) (string, bool) {
	decrypted, err := crypto.DecryptString(value)
	return decrypted, err == nil
}

func normalizedRMCCookie(input string) (string, bool) {
	cookie, err := normalizeRMCCookie(input)
	return cookie, err == nil
}

func (g *GameBanana) persistManualCookie(ctx context.Context, cookie string) bool {
	return g.saveCookie(ctx, cookie) == nil
}

func (g *GameBanana) saveCookie(ctx context.Context, cookie string) error {
	rmc := cookieValue(cookie, "rmc")
	if rmc == "" {
		return ErrInvalidRMC
	}
	g.mu.Lock()
	client := g.client
	crypto := g.crypto
	g.mu.Unlock()
	if client == nil || crypto == nil {
		return errors.New("GameBanana cookie store is not configured")
	}
	stored := "rmc=" + rmc
	encrypted, err := crypto.EncryptString(stored)
	if err != nil {
		return err
	}
	if err := client.Settings.Upsert(ctx, cookieSettingKey, &encrypted); err != nil {
		return err
	}
	g.mu.Lock()
	g.sessionCookie = cookie
	g.mu.Unlock()
	return nil
}

func (g *GameBanana) removeCookie(ctx context.Context) error {
	g.mu.Lock()
	g.sessionCookie = ""
	client := g.client
	g.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Settings.Upsert(ctx, cookieSettingKey, nil)
}

func normalizeRMCCookie(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ErrInvalidRMC
	}
	var token string
	found := false
	for _, segment := range strings.Split(trimmed, ";") {
		segment = strings.TrimSpace(segment)
		if strings.HasPrefix(segment, "rmc=") {
			token = strings.TrimSpace(strings.TrimPrefix(segment, "rmc="))
			found = true
			break
		}
	}
	if !found {
		token = strings.TrimSpace(strings.TrimPrefix(trimmed, "rmc="))
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", ErrInvalidRMC
	}
	return "rmc=" + token, nil
}

func cookieValue(cookie, name string) string {
	for _, segment := range strings.Split(cookie, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(segment), "=")
		if ok && key == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mergeSetCookies(current string, headers []string) string {
	values := make(map[string]string)
	for _, segment := range strings.Split(current, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(segment), "=")
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	for _, header := range headers {
		cookie := strings.SplitN(header, ";", 2)[0]
		key, value, ok := strings.Cut(strings.TrimSpace(cookie), "=")
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, "; ")
}

func normalizeModelName(value string) (string, error) {
	if value == "" {
		value = "Mod"
	}
	if !submissionModelPattern.MatchString(value) {
		return "", errors.New("invalid GameBanana model name")
	}
	return value, nil
}

func applyModIndexDefaults(input *ModIndexInput) {
	if input.PerPage < 1 {
		input.PerPage = 15
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.Sort == "" {
		input.Sort = "Generic_Newest"
	}
}

func gameReferer(gameID int) string { return fmt.Sprintf("https://gamebanana.com/games/%d", gameID) }
func categoryReferer(categoryID int) string {
	return fmt.Sprintf("https://gamebanana.com/mods/cats/%d", categoryID)
}
func submissionReferer(model string, itemID int) string {
	return fmt.Sprintf("https://gamebanana.com/%ss/%d", strings.ToLower(model), itemID)
}

func sanitizeLogMessage(msg string) string {
	if msg == "" {
		return msg
	}
	// Never log cookie values or raw Cookie / Set-Cookie headers.
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "rmc=") || strings.Contains(lower, "cookie:") || strings.Contains(lower, "set-cookie") {
		return "gamebanana request failed"
	}
	return msg
}

func cloneRecord(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

// normalizePreviewContent rewrites every _aPreviewContent record in a decoded
// response into the {"screenshots": [...]} shape the frontend expects. The
// GameBanana API returns a single screenshot object for index and subfeed
// records, while ProfilePage responses already use a screenshots array.
func normalizePreviewContent(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if preview, ok := typed["_aPreviewContent"]; ok {
			typed["_aPreviewContent"] = normalizePreviewMedia(preview)
		}
		for key, child := range typed {
			typed[key] = normalizePreviewContent(child)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = normalizePreviewContent(child)
		}
		return typed
	default:
		return value
	}
}

func normalizePreviewMedia(value any) any {
	switch typed := value.(type) {
	case []any:
		return map[string]any{"screenshots": typed}
	case map[string]any:
		if _, ok := typed["screenshots"]; ok {
			return typed
		}
		if screenshot, ok := typed["screenshot"]; ok {
			return map[string]any{"screenshots": []any{screenshot}}
		}
		return typed
	default:
		return value
	}
}
