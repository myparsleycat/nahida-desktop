//go:build !windows

package app

import "errors"

func spawnDetachedRelaunch(string, int) error {
	return errors.New("restart is only available on Windows")
}

func relaunchProcessAlive(int) bool {
	return false
}
