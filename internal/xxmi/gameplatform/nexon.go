package gameplatform

// NexonLaunchURI returns the request that makes Nexon Game Manager start the game with the given number.
func NexonLaunchURI(gameID string) string {
	return "ngm://launch/ -mode:pluglaunch -game:" + gameID
}
