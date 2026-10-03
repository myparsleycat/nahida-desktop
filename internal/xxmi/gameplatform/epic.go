package gameplatform

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// EpicURIPrefix starts every request that asks the Epic Games Launcher to run an installed application.
const EpicURIPrefix = "com.epicgames.launcher://apps/"

// EpicApp is a game installed through the Epic Games Launcher.
type EpicApp struct {
	NamespaceID string
	ItemID      string
	ArtifactID  string
	InstallDir  string
}

// FindEpicGame looks the game up in the launcher's installation list by its install folder name.
// found is false when the launcher has no such game. A missing list, reported as fs.ErrNotExist,
// means the launcher is not installed.
func FindEpicGame(manifestPath string, game Game) (app EpicApp, found bool, err error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return EpicApp{}, false, err
	}
	var manifest struct {
		InstallationList []struct {
			InstallLocation string `json:"InstallLocation"`
			NamespaceID     string `json:"NamespaceId"`
			ItemID          string `json:"ItemId"`
			ArtifactID      string `json:"ArtifactId"`
		} `json:"InstallationList"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &manifest); err != nil {
		return EpicApp{}, false, err
	}
	for _, entry := range manifest.InstallationList {
		if entry.InstallLocation == "" || entry.NamespaceID == "" || entry.ItemID == "" || entry.ArtifactID == "" {
			continue
		}
		location := filepath.Clean(filepath.FromSlash(entry.InstallLocation))
		if game.matches(filepath.Base(location)) {
			return EpicApp{
				NamespaceID: entry.NamespaceID, ItemID: entry.ItemID, ArtifactID: entry.ArtifactID,
				InstallDir: location,
			}, true, nil
		}
	}
	return EpicApp{}, false, nil
}

// LaunchURI returns the request that makes the launcher start the game with the given arguments.
func (a EpicApp) LaunchURI(args string) string {
	uri := EpicURIPrefix + a.NamespaceID + "%3A" + a.ItemID + "%3A" + a.ArtifactID + "?action=launch&silent=true"
	if args = strings.TrimSpace(args); args != "" {
		// The arguments travel inside a quoted query value, so the characters that would end it are encoded too.
		encoded := strings.NewReplacer("%", "%25", " ", "%20", `"`, "%22", "&", "%26", "#", "%23").Replace(args)
		uri += `&args="` + encoded + `"`
	}
	return uri
}
