package common

// These variables are injected at build time using -ldflags
var (
	SUMMARY = "development"
	BRANCH  = "unknown"
	VERSION = "dev"
	COMMIT  = "unknown"
)

func GetVersion() string {
	if VERSION == "dev" {
		return "1.0.0-dev"
	}
	return VERSION
}
