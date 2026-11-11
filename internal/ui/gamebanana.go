package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
)

const (
	gbModsSortNewest     = "Generic_Newest"
	gbCommentsPerPage    = 15
	gbImageFetchTimeout  = 30 * time.Second
	gbImageMaxBytes      = 32 << 20
	gbMaxVisiblePreviews = 8
)

// gbNumber decodes a JSON number or a numeric string, as GameBanana sends row ids and counts both ways.
type gbNumber int64

func (n *gbNumber) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		*n = 0
		return nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*n = gbNumber(value)
	return nil
}

// gbText decodes a JSON string or number as text.
type gbText string

func (t *gbText) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*t = gbText(text)
		return nil
	}
	*t = gbText(strings.TrimSpace(string(data)))
	return nil
}

type gbNamed struct {
	Name       string `json:"_sName"`
	AvatarURL  string `json:"_sAvatarUrl"`
	ProfileURL string `json:"_sProfileUrl"`
}

type gbPreviewImage struct {
	BaseURL string `json:"_sBaseUrl"`
	URL     string `json:"_sUrl"`
	Caption string `json:"_sCaption"`
	File    string `json:"_sFile"`
	File100 string `json:"_sFile100"`
	File220 string `json:"_sFile220"`
	File530 string `json:"_sFile530"`
}

type gbPreviewMedia struct {
	Screenshots []gbPreviewImage `json:"screenshots"`
}

type gbSubmission struct {
	ID           gbNumber        `json:"_idRow"`
	ModelName    string          `json:"_sModelName"`
	Name         string          `json:"_sName"`
	Description  string          `json:"_sDescription"`
	Preview      *gbPreviewMedia `json:"_aPreviewContent"`
	Submitter    gbNamed         `json:"_aSubmitter"`
	RootCategory *gbNamed        `json:"_aRootCategory"`
	SubCategory  *gbNamed        `json:"_aSubCategory"`
	DateAdded    *gbNumber       `json:"_tsDateAdded"`
	DateModified *gbNumber       `json:"_tsDateModified"`
	DateUpdated  *gbNumber       `json:"_tsDateUpdated"`
	LikeCount    *gbNumber       `json:"_nLikeCount"`
	PostCount    *gbNumber       `json:"_nPostCount"`
	ViewCount    *gbNumber       `json:"_nViewCount"`
}

type gbMetadata struct {
	RecordCount gbNumber `json:"_nRecordCount"`
	PerPage     gbNumber `json:"_nPerpage"`
	IsComplete  bool     `json:"_bIsComplete"`
}

type gbFeed struct {
	Metadata gbMetadata     `json:"_aMetadata"`
	Records  []gbSubmission `json:"_aRecords"`
}

type gbCategory struct {
	ID        gbNumber  `json:"_idRow"`
	Name      string    `json:"_sName"`
	IconURL   string    `json:"_sIconUrl"`
	ItemCount *gbNumber `json:"_nItemCount"`
}

type gbGameOverview struct {
	Profile struct {
		Name           string       `json:"_sName"`
		ProfileURL     string       `json:"_sProfileUrl"`
		RootCategories []gbCategory `json:"_aModRootCategories"`
	} `json:"profile"`
}

type gbCategoryOverview struct {
	Profile struct {
		Name       string `json:"_sName"`
		ProfileURL string `json:"_sProfileUrl"`
	} `json:"profile"`
	Index      gbFeed       `json:"index"`
	Categories []gbCategory `json:"categories"`
}

type gbFile struct {
	ID            gbNumber `json:"_idRow"`
	File          string   `json:"_sFile"`
	DownloadCount gbNumber `json:"_nDownloadCount"`
	DateAdded     gbNumber `json:"_tsDateAdded"`
}

type gbModOverview struct {
	Profile struct {
		ID               gbNumber        `json:"_idRow"`
		Name             string          `json:"_sName"`
		ProfileURL       string          `json:"_sProfileUrl"`
		Text             string          `json:"_sText"`
		Preview          *gbPreviewMedia `json:"_aPreviewContent"`
		Category         gbNamed         `json:"_aCategory"`
		Submitter        gbNamed         `json:"_aSubmitter"`
		Game             gbNamed         `json:"_aGame"`
		LikeCount        gbNumber        `json:"_nLikeCount"`
		PostCount        gbNumber        `json:"_nPostCount"`
		ViewCount        gbNumber        `json:"_nViewCount"`
		DownloadCount    gbNumber        `json:"_nDownloadCount"`
		AccessorHasLiked *bool           `json:"_bAccessorHasLiked"`
		Files            []gbFile        `json:"_aFiles"`
	} `json:"profile"`
	// markdown is Profile.Text converted once after decoding, off the UI thread.
	markdown string
	Config   struct {
		Access map[string]bool `json:"_aAccess"`
	} `json:"config"`
}

type gbPost struct {
	ID         gbNumber  `json:"_idRow"`
	Status     gbText    `json:"_nStatus"`
	Text       string    `json:"_sText"`
	Poster     *gbNamed  `json:"_aPoster"`
	DateAdded  *gbNumber `json:"_tsDateAdded"`
	ReplyCount *gbNumber `json:"_nReplyCount"`
	Stamps     []struct {
		Count *gbNumber `json:"_nCount"`
	} `json:"_aStamps"`
	// body is Text as plain text, converted once after decoding.
	body string
}

type gbPosts struct {
	Metadata gbMetadata `json:"_aMetadata"`
	Records  []gbPost   `json:"_aRecords"`
}

// gbDecode converts the generic JSON value a GameBanana call returns into a typed view of it.
func gbDecode[T any](value any) (*T, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("GAMEBANANA_SCHEMA_ERROR:decode:%w", err)
	}
	if p, ok := any(&out).(interface{ prepare() }); ok {
		p.prepare()
	}
	return &out, nil
}

func (m *gbModOverview) prepare() { m.markdown = gbHTMLToMarkdown(m.Profile.Text) }

func (p *gbPosts) prepare() {
	for i := range p.Records {
		post := &p.Records[i]
		if post.body = gbHTMLToText(post.Text); post.body == "" {
			post.body = post.Text
		}
	}
}

// gbQuery is one cached request. key identifies its inputs; a different key starts a new load and drops a
// late answer to the old one. A new generation loads the same key again in the background, keeping what is
// shown, as React Query refetched invalidated queries.
type gbQuery[T any] struct {
	key string
	gen int
	// loading is set while a load runs with nothing to show; a refetch of shown data leaves it unset.
	loading bool
	data    *T
	err     error
	// pages keeps earlier answers by key, shown at once while they load again, as React kept each page in its
	// query cache.
	pages map[string]*T
}

const (
	gbQueryCacheSize = 32
	gbRetries        = 3
)

type gbCrumb struct {
	id   int
	name string
}

type gbSelection struct {
	id    int
	model string
}

func (sel gbSelection) key() string { return sel.model + ":" + strconv.Itoa(sel.id) }

type gameBananaScreen struct {
	// generation changes when the session changes, so every query loads again, as the React route
	// invalidated its "gamebanana" queries.
	generation int

	session gbQuery[gamebanana.SessionStatus]

	game           string
	categoryID     int
	breadcrumbs    []gbCrumb
	categorySearch string
	mod            *gbSelection
	subfeedPage    int
	modsPage       int
	modSearch      string
	modsSort       string
	modURLOpen     bool
	modURL         string
	modURLError    string
	// syncFromMod picks the game the mod screen shows the next time the screen builds.
	syncFromMod bool
	// savedView is the view as last stored in the renderer state.
	savedView string

	overview gbQuery[gbGameOverview]
	subfeed  gbQuery[gbFeed]
	category gbQuery[gbCategoryOverview]
	modView  gbQuery[gbModOverview]

	commentSort  string
	posts        []gbPosts
	postsKey     string
	postsGen     int
	postsLoading bool
	postsErr     error

	signingIn     bool
	loggingOut    bool
	logoutConfirm bool
	manualOpen    bool
	manualValue   string
	manualError   string
	manualSaving  bool

	likePending  string
	likeOverride *gbLikeOverride
	pendingFile  int
	lightbox     int
	lightboxKey  string
	// pageEdits holds the text typed into a pagination field until it is committed.
	pageEdits map[string]string
}

type gbLikeOverride struct {
	key   string
	liked bool
}

func newGameBananaScreen() gameBananaScreen {
	return gameBananaScreen{
		subfeedPage: 1, modsPage: 1, modsSort: gbModsSortNewest, commentSort: "popular",
		lightbox: -1, pageEdits: map[string]string{},
	}
}

// gbLoad starts loading q when its key or the screen's generation changed. fetch runs off the UI thread.
func gbLoad[T any](s *Shell, q *gbQuery[T], key string, operation string, fetch func(context.Context) (any, error)) {
	gen := s.gameBanana.generation
	if q.key == key && q.gen == gen {
		return
	}
	if q.key != key {
		q.data, q.err = q.pages[key], nil
	}
	q.key, q.gen, q.loading = key, gen, q.data == nil
	go func() {
		data, err := gbFetchRetrying(func(ctx context.Context) (*T, error) {
			value, err := fetch(ctx)
			if err != nil {
				return nil, err
			}
			return gbDecode[T](value)
		})
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(s.deps.Log, err, "GameBanana", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: operation, Fields: map[string]any{"key": key},
			})
		}
		fairy.Go(func() {
			if q.key != key || q.gen != gen {
				return
			}
			q.loading = false
			// A failed refetch keeps the answer already shown.
			if err != nil {
				if q.data == nil {
					q.err = err
				}
				return
			}
			q.data, q.err = data, nil
			if q.pages == nil || len(q.pages) >= gbQueryCacheSize {
				q.pages = map[string]*T{}
			}
			q.pages[key] = data
		})
	}()
}

// gbFetchRetrying tries fetch up to three more times with growing delays, as React Query retried failed
// queries. An answer the server will repeat, such as a missing sign-in or an unexpected schema, is not retried.
func gbFetchRetrying[T any](fetch func(context.Context) (T, error)) (T, error) {
	var (
		value T
		err   error
	)
	for attempt := range gbRetries + 1 {
		if attempt > 0 {
			time.Sleep(time.Second << (attempt - 1))
		}
		if value, err = fetch(context.Background()); err == nil || !gbRetryable(err) {
			return value, err
		}
	}
	return value, err
}

func gbRetryable(err error) bool {
	if infra.IsCancellationError(err) {
		return false
	}
	message := strings.TrimSpace(err.Error())
	return message != gamebanana.ErrAuthRequired.Error() && message != gamebanana.ErrAuthFailed.Error() &&
		!strings.HasPrefix(message, "GAMEBANANA_SCHEMA_ERROR:")
}

func (s *Shell) startGameBanana() {
	g := &s.gameBanana
	gbLoad(s, &g.session, "session", "session-status", func(ctx context.Context) (any, error) {
		return s.deps.GameBanana.GetSessionStatus(ctx)
	})
	games := s.deps.GameBanana.GetGameRegistry()
	if len(games) == 0 {
		return
	}
	// Coming from the mod screen opens the home of its game even when it is the game shown, as React set the
	// selected game again.
	if g.syncFromMod {
		g.syncFromMod = false
		if key := s.gameBananaKeyForModGame(games); key != "" {
			s.gbSelectGame(key)
		}
	}
	if !gbHasGame(games, g.game) {
		g.game = games[0].Key
	}
	gameID := gbGameID(games, g.game)
	gbLoad(s, &g.overview, strconv.Itoa(gameID), "game-overview", func(ctx context.Context) (any, error) {
		return s.deps.GameBanana.GetGameOverview(ctx, gameID)
	})
	page := g.subfeedPage
	gbLoad(s, &g.subfeed, fmt.Sprint(gameID, ":", page), "game-subfeed", func(ctx context.Context) (any, error) {
		return s.deps.GameBanana.GetGameSubfeed(ctx, gamebanana.GameSubfeedInput{GameID: gameID, Page: page})
	})
	if g.categoryID > 0 {
		id, page, sort := g.categoryID, g.modsPage, g.modsSort
		gbLoad(s, &g.category, fmt.Sprint(id, ":", page, ":", sort), "category-overview",
			func(ctx context.Context) (any, error) {
				return s.deps.GameBanana.GetModCategoryOverview(ctx, gamebanana.ModCategoryOverviewInput{
					CategoryID: id, Page: page, ModSort: sort,
				})
			})
	}
	if g.mod != nil {
		sel := *g.mod
		gbLoad(s, &g.modView, sel.key(), "mod-overview", func(ctx context.Context) (any, error) {
			return s.deps.GameBanana.GetModOverview(ctx, gamebanana.ModOverviewInput{
				ItemID: sel.id, ModelName: sel.model,
			})
		})
		s.gbLoadPosts(sel.key()+":"+g.commentSort, sel)
	}
}

// gbLoadPosts loads the first page of comments under a new key. A new generation loads every page shown
// again and swaps them in together, as React refetched each page of an infinite query.
func (s *Shell) gbLoadPosts(key string, sel gbSelection) {
	g := &s.gameBanana
	gen := g.generation
	if g.postsKey == key && g.postsGen == gen {
		return
	}
	pages := 1
	if g.postsKey == key {
		pages = max(len(g.posts), 1)
	} else {
		g.posts, g.postsErr = nil, nil
	}
	g.postsKey, g.postsGen = key, gen
	s.gbFetchPosts(key, gen, sel, 1, pages)
}

func (s *Shell) gbLoadMorePosts() {
	g := &s.gameBanana
	if g.mod == nil || len(g.posts) == 0 || g.postsLoading {
		return
	}
	s.gbFetchPosts(g.postsKey, g.postsGen, *g.mod, len(g.posts)+1, 1)
}

// gbFetchPosts loads count pages from first on. Pages from 1 replace the list; later ones append.
func (s *Shell) gbFetchPosts(key string, gen int, sel gbSelection, first, count int) {
	g := &s.gameBanana
	g.postsLoading = true
	sort := g.commentSort
	go func() {
		var (
			loaded []gbPosts
			err    error
		)
		for page := first; page < first+count && err == nil; page++ {
			var posts *gbPosts
			posts, err = gbFetchRetrying(func(ctx context.Context) (*gbPosts, error) {
				value, err := s.deps.GameBanana.GetModPosts(ctx, gamebanana.ModPostsInput{
					ModID: sel.id, ModelName: sel.model, Page: page, PerPage: gbCommentsPerPage, Sort: sort,
				})
				if err != nil {
					return nil, err
				}
				return gbDecode[gbPosts](value)
			})
			if err == nil {
				loaded = append(loaded, *posts)
			}
		}
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(s.deps.Log, err, "GameBanana", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "mod-posts",
				Fields: map[string]any{"itemId": sel.id, "model": sel.model, "page": first},
			})
		}
		fairy.Go(func() {
			if g.postsKey != key || g.postsGen != gen {
				return
			}
			g.postsLoading = false
			switch {
			case err != nil:
				if len(g.posts) == 0 {
					g.postsErr = err
				}
			case first == 1:
				g.posts, g.postsErr = loaded, nil
			case len(g.posts) == first-1:
				g.posts = append(g.posts, loaded...)
			}
		})
	}()
}

// gbRefresh loads every GameBanana query again in the background: after the session changed, on a retry, and
// when the window comes back to the front, as React Query refetched on focus.
func (s *Shell) gbRefresh() {
	g := &s.gameBanana
	g.generation++
}

// gbRefetchMod loads the open mod again and waits for it. It runs off the UI thread.
func (s *Shell) gbRefetchMod(ctx context.Context, sel gbSelection) error {
	value, err := s.deps.GameBanana.GetModOverview(ctx, gamebanana.ModOverviewInput{
		ItemID: sel.id, ModelName: sel.model,
	})
	var data *gbModOverview
	if err == nil {
		data, err = gbDecode[gbModOverview](value)
	}
	fairy.Go(func() {
		g := &s.gameBanana
		if g.mod == nil || *g.mod != sel || err != nil {
			return
		}
		g.modView.loading, g.modView.data, g.modView.err = false, data, nil
	})
	return err
}

func gbHasGame(games []gamebanana.GameRegistration, key string) bool {
	return gbGameID(games, key) != 0
}

func gbGameID(games []gamebanana.GameRegistration, key string) int {
	for _, game := range games {
		if game.Key == key {
			return game.ID
		}
	}
	return 0
}

// gameBananaKeyForModGame returns the GameBanana game of the importer the mod screen's game uses.
func (s *Shell) gameBananaKeyForModGame(games []gamebanana.GameRegistration) string {
	var config *mod.GameConfig
	for i := range s.mods.games {
		if s.mods.games[i].Game == s.mods.game {
			config = &s.mods.games[i]
		}
	}
	if config == nil || config.Importer == nil {
		return ""
	}
	for _, game := range games {
		if game.Importer == *config.Importer {
			return game.Key
		}
	}
	return ""
}

func (s *Shell) gbSelectGame(key string) {
	g := &s.gameBanana
	g.game = key
	g.categoryID, g.breadcrumbs, g.categorySearch, g.mod = 0, nil, "", nil
	g.subfeedPage, g.modsPage, g.modSearch = 1, 1, ""
}

func (s *Shell) gbSelectCategory(id int, name string) {
	g := &s.gameBanana
	if g.categoryID == 0 {
		g.breadcrumbs = []gbCrumb{{id, name}}
	} else {
		g.breadcrumbs = append(g.breadcrumbs, gbCrumb{id, name})
	}
	g.categoryID, g.categorySearch, g.mod, g.modsPage, g.modSearch = id, "", nil, 1, ""
}

func (s *Shell) gbSelectBreadcrumb(index int) {
	g := &s.gameBanana
	if index < 0 || index >= len(g.breadcrumbs) {
		return
	}
	g.breadcrumbs = g.breadcrumbs[:index+1]
	g.categoryID = g.breadcrumbs[index].id
	g.categorySearch, g.mod, g.modsPage, g.modSearch = "", nil, 1, ""
}

func (s *Shell) gbResetToGameHome() {
	g := &s.gameBanana
	g.categoryID, g.breadcrumbs, g.categorySearch, g.mod = 0, nil, "", nil
	g.subfeedPage, g.modsPage, g.modSearch = 1, 1, ""
}

func (s *Shell) gbSelectMod(id int, model string) {
	if model == "" {
		model = "Mod"
	}
	g := &s.gameBanana
	sel := gbSelection{id: id, model: model}
	// Each mod opens with popular comments, as React mounted its detail panel anew per mod.
	if g.mod == nil || *g.mod != sel {
		g.commentSort = "popular"
	}
	g.mod = &sel
	g.lightbox = -1
}

var gbModPathPattern = regexp.MustCompile(`(?i)^/mods/(\d+)/?$`)

// parseGameBananaModURL accepts an http(s) gamebanana.com mod page without credentials.
func parseGameBananaModURL(value string) (int, bool) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return 0, false
	}
	host := strings.ToLower(u.Hostname())
	if host != "gamebanana.com" && host != "www.gamebanana.com" {
		return 0, false
	}
	match := gbModPathPattern.FindStringSubmatch(u.Path)
	if match == nil {
		return 0, false
	}
	id, err := strconv.Atoi(match[1])
	return id, err == nil && id > 0
}

func (s *Shell) gbOpenModURL() {
	g := &s.gameBanana
	id, ok := parseGameBananaModURL(g.modURL)
	if !ok {
		g.modURLError = T("page.gamebanana.open_mod_url.invalid")
		return
	}
	s.gbSelectMod(id, "Mod")
	g.modURL, g.modURLError = "", ""
}

// gbAuthErrorCode maps an error onto the GameBanana sign-in codes the screen distinguishes.
func gbAuthErrorCode(err error) string {
	for _, code := range []error{
		gamebanana.ErrAuthRequired, gamebanana.ErrLoginInitFailed, gamebanana.ErrAuthCheckFailed,
		gamebanana.ErrLoginCancelled, gamebanana.ErrAutoLoginUnsupported, gamebanana.ErrServerUnreachable,
	} {
		if errors.Is(err, code) || (err != nil && strings.TrimSpace(err.Error()) == code.Error()) {
			return code.Error()
		}
	}
	return gamebanana.ErrAuthFailed.Error()
}

// gbAuthCopy returns the toast title and description keys for a sign-in failure code.
func gbAuthCopy(code string) (string, string) {
	const prefix = "page.gamebanana.auth."
	switch code {
	case gamebanana.ErrLoginInitFailed.Error():
		return prefix + "init_failed_title", prefix + "init_failed_description"
	case gamebanana.ErrAuthCheckFailed.Error():
		return prefix + "check_failed_title", prefix + "check_failed_description"
	case gamebanana.ErrAutoLoginUnsupported.Error():
		return prefix + "unsupported_title", prefix + "unsupported_description"
	case gamebanana.ErrServerUnreachable.Error():
		return prefix + "unreachable_title", prefix + "unreachable_description"
	}
	return prefix + "required_title", prefix + "required_description"
}

func (s *Shell) gbAuthFailureToast(err error) {
	code := gbAuthErrorCode(err)
	// The user closed the login window or a logout won the race; neither is a failure.
	if code == gamebanana.ErrLoginCancelled.Error() {
		return
	}
	title, description := gbAuthCopy(code)
	fairy.Go(func() { s.toasts.Show(kit.ToastError, T(title), T(description)) })
}

// gbSignIn opens the login window and waits for it. Every action that needs an account signs in through
// it, so the failure toast and the refresh after a new session stay identical. It runs off the UI thread.
func (s *Shell) gbSignIn(ctx context.Context) bool {
	if err := s.deps.GameBanana.EnsureSession(ctx); err != nil {
		s.gbAuthFailureToast(err)
		return false
	}
	fairy.Go(s.gbRefresh)
	return true
}

func (s *Shell) gbSignInFromToolbar() {
	g := &s.gameBanana
	if g.signingIn {
		return
	}
	g.signingIn = true
	go func() {
		s.gbSignIn(context.Background())
		fairy.Go(func() { g.signingIn = false })
	}()
}

func (s *Shell) gbLogout() {
	g := &s.gameBanana
	g.loggingOut = true
	go func() {
		err := s.deps.GameBanana.Logout(context.Background())
		if err != nil {
			s.gbAuthFailureToast(err)
		}
		fairy.Go(func() {
			g.loggingOut = false
			s.gbRefresh()
		})
	}()
}

var gbManualRMCCodes = []string{
	"GAMEBANANA_INVALID_RMC", "GAMEBANANA_SERVER_UNREACHABLE", "GAMEBANANA_MANUAL_RMC_SAVE_FAILED",
	"GAMEBANANA_AUTH_CHECK_FAILED", "GAMEBANANA_LOGIN_INIT_FAILED",
}

func gbManualRMCMessage(code string) string {
	for _, known := range gbManualRMCCodes {
		if code == known {
			return T("page.gamebanana.auth.manual_rmc." + code)
		}
	}
	return T("page.gamebanana.auth.manual_rmc.GAMEBANANA_MANUAL_RMC_SAVE_FAILED")
}

func (s *Shell) gbSaveManualRMC() {
	g := &s.gameBanana
	value := strings.TrimSpace(g.manualValue)
	if value == "" {
		g.manualError = T("page.gamebanana.auth.manual_rmc.empty")
		return
	}
	g.manualSaving, g.manualError = true, ""
	go func() {
		result, err := s.deps.GameBanana.SetManualRMCToken(context.Background(), value)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "GameBanana", infra.Diagnostic{Operation: "manual-rmc"})
		}
		fairy.Go(func() {
			g.manualSaving = false
			switch {
			case err != nil:
				g.manualError = gbManualRMCMessage(strings.TrimSpace(err.Error()))
			case !result.OK:
				g.manualError = gbManualRMCMessage(result.ErrorCode)
			default:
				g.manualOpen, g.manualValue = false, ""
				s.gbRefresh()
			}
		})
	}()
}

// gbToggleLike signs in first when the mod does not offer the like yet, as the React panel did.
func (s *Shell) gbToggleLike(sel gbSelection, canToggle, signedIn bool) {
	g := &s.gameBanana
	key := sel.key()
	if g.likePending == key || (!canToggle && signedIn) {
		return
	}
	g.likePending = key
	go func() {
		ctx := context.Background()
		defer fairy.Go(func() {
			if g.likePending == key {
				g.likePending = ""
			}
		})
		if !canToggle {
			if !s.gbSignIn(ctx) {
				return
			}
			_ = s.gbRefetchMod(ctx, sel)
		}
		result, err := s.deps.GameBanana.ToggleModLike(ctx, gamebanana.ModOverviewInput{
			ItemID: sel.id, ModelName: sel.model,
		})
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "GameBanana", infra.Diagnostic{
				Operation: "toggle-like", Fields: map[string]any{"itemId": sel.id, "model": sel.model},
			})
			fairy.Go(func() { s.toasts.Show(kit.ToastError, T("page.gamebanana.like_failed"), err.Error()) })
			return
		}
		fairy.Go(func() { g.likeOverride = &gbLikeOverride{key: key, liked: result.Liked} })
		if s.gbRefetchMod(ctx, sel) == nil {
			fairy.Go(func() {
				if g.likeOverride != nil && g.likeOverride.key == key {
					g.likeOverride = nil
				}
			})
		}
	}()
}

// gbDownload starts a file download, signing in and trying once more when the file needs an account.
func (s *Shell) gbDownload(itemID, fileID int) {
	g := &s.gameBanana
	if g.pendingFile != 0 {
		return
	}
	g.pendingFile = fileID
	s.background(func(ctx context.Context) {
		props := mod.GameBananaDownloadProps{ItemID: itemID, FileID: fileID, ModelName: "Mod"}
		_, err := s.deps.Mod.DownloadGameBananaFile(ctx, props)
		if err != nil && gbAuthErrorCode(err) == gamebanana.ErrAuthRequired.Error() {
			err = nil
			if s.gbSignIn(ctx) {
				_, err = s.deps.Mod.DownloadGameBananaFile(ctx, props)
			}
		}
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(s.deps.Log, err, "GameBanana", infra.Diagnostic{
				Operation: "download", Fields: map[string]any{"itemId": itemID, "fileId": fileID},
			})
			fairy.Go(func() { s.toasts.Show(kit.ToastError, T("page.gamebanana.download_failed"), err.Error()) })
		}
		fairy.Go(func() { g.pendingFile = 0 })
	})
}

func (s *Shell) gbOpenExternal(link string) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		return
	}
	go func() {
		if err := s.deps.Platform.OpenExternal(link); err != nil {
			_ = infra.ReportError(s.deps.Log, err, "GameBanana", infra.Diagnostic{
				Operation: "open-external", Fields: map[string]any{"url": infra.SanitizeLogURL(link)},
			})
		}
	}()
}

// gbImage returns a remote picture decoded for a w×h box drawn with fit, or nil while it loads or when it failed.
func (s *Shell) gbImage(src string, w, h float32, fit fairy.Fit) *fairy.Image {
	u, err := url.Parse(src)
	if src == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	return fairy.Images.Sized("gamebanana:"+src, w, h, fit, func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), gbImageFetchTimeout)
		defer cancel()
		response, err := s.deps.HTTP.Fetch(ctx, src, infra.FetchOptions{})
		if err != nil {
			return nil, err
		}
		defer func() { _ = response.Body.Close() }()
		return io.ReadAll(io.LimitReader(response.Body, gbImageMaxBytes))
	})
}

// gbErrorPresentation explains a load failure as the React screens did.
func gbErrorPresentation(err error) (string, string) {
	if err == nil {
		return T("page.gamebanana.error_description_unknown"), ""
	}
	message := strings.TrimSpace(err.Error())
	switch {
	case message == "":
		return T("page.gamebanana.error_description_unknown"), ""
	case message == gamebanana.ErrAuthRequired.Error() || message == gamebanana.ErrAuthFailed.Error():
		return T("page.gamebanana.error_description_auth"), ""
	case strings.HasPrefix(message, "GAMEBANANA_HTTP_ERROR:"):
		parts := strings.SplitN(message, ":", 3)
		status, statusText := "", ""
		if len(parts) > 1 {
			status = parts[1]
		}
		if len(parts) > 2 && parts[2] != "" {
			statusText = " " + parts[2]
		}
		return T("page.gamebanana.error_description_http", "status", status, "statusText", statusText), ""
	case strings.HasPrefix(message, "GAMEBANANA_SCHEMA_ERROR:"):
		parts := strings.SplitN(message, ":", 3)
		schema, details := "", ""
		if len(parts) > 1 {
			schema = strings.ReplaceAll(parts[1], "_", " ")
		}
		if len(parts) > 2 {
			details = strings.TrimSpace(parts[2])
		}
		return T("page.gamebanana.error_description_schema", "context", schema), details
	}
	return T("page.gamebanana.error_description_unknown"), message
}
