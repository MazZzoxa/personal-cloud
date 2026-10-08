package config

type Config struct {
	RootDir    string
	DataDir    string
	StorageDir string
	WebDir     string
	ChatDir    string
	Host       string
	Port       int

	// TrustLocalhost makes direct connections from the host PC trusted
	// without pairing.
	TrustLocalhost bool
}
