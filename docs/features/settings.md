# Settings

Open `Settings` at the bottom of the navigation rail. The sections are listed on the left. The app version and `Check for updates` are at the bottom of the section list.

Most settings are saved as soon as they are changed. The proxy, the GitHub token, and the Nahida Agent settings are saved with their own `Save` buttons.

The `Mod` section is described in [Mod Settings](/features/mod-manager/settings). XXMI settings are on the XXMI page; see [Set Up XXMI](/others/set-up-xxmi).

## General {#general}

### Application

| Setting | Default | Description |
|---|---|---|
| `Run at sign-in` | Off | Starts the app when you sign in to Windows. Shown only in the installed version. |
| `Elevated helper` | Off | Starts a separate helper with administrator rights for actions that need them. Windows shows a UAC prompt when the app starts. |
| `Run in background` | On | Keeps the app running in the background when all windows are closed. |
| `Click titlebar activity badges to open` | On | Clicking an activity badge in the title bar opens the related page. When off, badges can be used to drag the window. |
| `Mica backdrop` | On | Uses the Windows Mica effect for the window background. Changing it reopens the window. Shown only when Windows supports Mica. |
| `Blur effect` | On | Blurs the content behind menus, dialogs, toasts, and overlays. Turn it off if the interface is slow. |
| `Language` | Windows language | `한국어`, `English`, `日本語`, or `中文`. The default is the Windows display language if it is one of these, and English otherwise. |
| `Theme` | `System default` | `System default`, `Light`, or `Dark`. |
| `Start page` | `Mod Manager` | The page shown when the app starts: `Transfer`, `GameBanana`, `Mod Manager`, `Drive`, or `Shared Drive`. `Drive` and `Shared Drive` require a nahida.live sign-in. |
| `Log level` | `Warn` | The lowest level written to the log: `Trace`, `Debug`, `Info`, `Warn`, `Error`, or `Fatal`. |

[Persist Toggles](/features/mod-tools/persist-toggles) and `Toggle Viewer Auto Generate` in Toggle Viewer Generator need the app to keep running. Turning either on also turns on `Run in background`, and turning `Run in background` off while either is on asks for confirmation.

### Sorting

| Setting | Default | Description |
|---|---|---|
| `Name sort policy` | `Natural sort + ignore spaces` | How spaces and numbers are handled when lists are sorted by name. Applies to every list sorted by name, including Drive, the mod sidebar and mod list, presets, and Fix Tool. |

- `Natural sort + ignore spaces`: compares numbers by value, and compares names without spaces first.
- `Default natural sort`: compares numbers by value.

### Updates

| Setting | Default | Description |
|---|---|---|
| `Auto update` | `Automatic update` | How app updates are handled. |
| `Include pre-releases` | Off | Also offers GitHub pre-releases as app updates. Draft releases are excluded. |
| `Update status` | | Shows the current update state. `Download update` or `Install update` appears when an update is available or downloaded. |

The options of `Auto update` are:

- `Automatic update`: Checks for updates at startup and every hour, and downloads them automatically.
- `Update notifications`: Checks for updates automatically and downloads them only when you choose to.
- `Disabled`: Does not check for or download updates automatically.

`Check for updates` at the bottom of the section list checks right away.

## Mod {#mod}

See [Mod Settings](/features/mod-manager/settings).

## Drive {#drive}

Settings for [Drive and Shared Drive](/features/#drive).

| Setting | Default | Description |
|---|---|---|
| `Auto-try passwords` | Off | Tries the saved passwords when a link that requires a password is resolved. |
| `Password list` | Empty | The passwords to try. Up to 10 can be saved. Click `Add` to add one. |

## Account {#account}

Shows the nahida.live account you are signed in with.

- `Sign in`: Shown when you are not signed in.
- `My Account`: Opens the account page on nahida.live.
- `Sign out`: Signs out of the account.

If the server cannot be reached, `Retry` checks the connection again.

## Transfer {#transfer}

Settings for the [Transfers](/features/#transfers) page.

| Setting | Default | Description |
|---|---|---|
| `Download concurrency` | 32 | How many files are downloaded at the same time in drive transfers. The range is 16–64. |
| `Upload concurrency` | 8 | How many files are uploaded at the same time in drive transfers. The range is 4–16. |
| `Download bandwidth limit (MiB/s)` | 0 | The total download speed limit across all transfers. 0 is unlimited. The range is 0–1024. |
| `Open the transfer page when a transfer starts` | Off | Opens the Transfers page when a file transfer starts. |
| `Prevent sleep mode` | Off | Keeps Windows from sleeping while a transfer or sync is in progress. |

## Network {#network}

### Proxy

Routes all external requests made by the app through a proxy. When it is off, the app uses the Windows system proxy settings. Browsers, games, and scripts started by the app are not affected.

| Setting | Description |
|---|---|
| `Enable proxy` | Turns the proxy on or off. |
| `Type` | `HTTP`, `SOCKS5`, or `SOCKS5h`. `SOCKS5` resolves destination names locally; `SOCKS5h` resolves them at the proxy. |
| `Host (without scheme)` | The proxy host, without a scheme, path, or port. |
| `Port` | An integer from 1 to 65535. |
| `Username (optional)` | Required when a password is set. |
| `Password (optional)` | `Remove password` deletes the saved password when you save; `Keep saved password` keeps it. |

Click `Save` to save. Proxy changes apply after the app restarts, and the `Restart required` dialog offers `Restart`.

If the saved proxy configuration cannot be read, external requests are blocked until you enter a valid configuration or turn the proxy off, save, and restart.

### GitHub Token

Without a token, GitHub API requests are limited to 60 per hour per IP address. A personal access token raises the limit to 5,000 per hour. The app reads only public repositories, so a fine-grained token needs no extra permissions.

1. Click `Create a token on GitHub` and create a token.
2. Enter it in `Personal access token` and click `Save`.

The token is encrypted for the current Windows user. `Remove token` deletes it. If GitHub rejects the saved token, or it cannot be decrypted for the current Windows user, requests are sent without authentication until you enter a new token.

## Tools {#tools}

| Setting | Default | Description |
|---|---|---|
| `Update Notifications` (Wuwa Mod Fixer) | On | Shows a notification when a [Wuwa Mod Fixer](/features/mod-manager/mod-tools#fix) update is available. Background updates run every hour regardless of this setting. |

## Nahida Agent {#nahida-agent}

Sets up the model that Nahida Agent uses. Nahida Agent appears in the navigation rail after a provider and model are configured.

### LLM Provider

| Setting | Description |
|---|---|
| `Provider` | A built-in provider from the model list, or `Custom`. Providers with saved credentials are marked `Connected`. |
| `Model` | Select a model from the list or enter a model ID. The model list date is shown below the field; `Refresh the model list` updates it. |
| `Reasoning` | The reasoning level. For built-in providers, only the levels the model supports are listed. |
| `Run agent scripts without asking` | Runs the Python and PowerShell scripts the agent writes without asking for approval each time. Scripts run with your Windows account's permissions and are not limited to the agent's folders. |
| `API key` | `Keep current value` (or `Keep configured key`), `Replace`, or `Clear`. Providers that support ChatGPT sign-in also offer `ChatGPT Pro/Plus`: click `Sign in with ChatGPT` and finish signing in in the browser. If the browser does not open, use `Copy link`. |

With `Custom`, these settings are also shown. Built-in providers take them from the model list.

| Setting | Description |
|---|---|
| `Protocol` | `OpenAI Responses`, `OpenAI-compatible`, or `Anthropic Messages`. |
| `Endpoint` | The API base URL. |
| `Model accepts image input` | Sends attached images and screenshots returned by tools to the model. Turn it on only if the model supports image input. |
| `Context window`, `Maximum output tokens` | The model's token limits. |
| `Custom headers` | Headers sent only with requests to the custom endpoint. `Store encrypted` encrypts a value. |

`Test connection` sends a test request and shows the model and latency. Click `Save` to save the provider settings.

### MCP

Adds MCP servers whose tools the agent can use. Enabled MCP tools run automatically and may have permissions outside Nahida's file sandbox.

Click `Add server` and set `Transport`:

- `stdio`: Enter the `Executable`, `Arguments (one per line)`, and `Working directory`.
- `Streamable HTTP`: Enter the server URL.
- `Blender (built-in)`: Connects to the Blender MCP add-on through its local socket. Start the bridge server in the add-on's preferences in Blender first.

`Add entry` adds a name and value; `Store encrypted` encrypts the value. Turn on `Enabled` and click `Save`. `Test connection` on a saved server shows how many tools it provides.

### Skills

Lists the built-in and user skills. Skills are loaded when the model requests them. `Open user skills folder` opens the folder for your own skills, and `Reload` reads it again.

## Advanced {#advanced}

Shows every stored setting as a key and value. Use `Search setting keys...` to filter by key, and the edit button to change a raw value.

::: warning
Values entered here are saved as they are. A wrong value can change how the app behaves.
:::

The proxy and the GitHub token are not listed; change them in [Network](#network). Values whose key contains `password`, `token`, `secret`, `credentials`, or `api_key` are shown as `********`.

## Report a Problem {#report-a-problem}

Sends a problem report to the developer.

| Item | Description |
|---|---|
| `Category` | `Desktop app problem`, `Nahida service problem`, or `XXMI library problem`. |
| `Details` | What went wrong and how to make it happen again. Required. |
| `Attachments` | `Add files` attaches images, videos, text files, and zip archives. `Add mod folder` attaches the folder of a mod that has the problem; it requires a nahida.live sign-in, and files of types the drive does not accept are left out. |
| `Attach logs` | On by default. Attaches the app logs. Sensitive information such as your PC user name is anonymized. |

The length and attachment limits are loaded from the server and shown next to each field. Click `Send` to send the report; `Cancel` stops a report that is being sent.
