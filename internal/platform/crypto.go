package platform

// Crypto is the Electron safeStorage port used for the auth token at rest.
type Crypto struct{}

func NewCrypto() *Crypto {
	return &Crypto{}
}
