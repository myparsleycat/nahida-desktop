package platform

import (
	"os"
	"strings"
)

// SystemLocale is the OS locale, matching Electron app.getSystemLocale().
func SystemLocale() string {
	if loc := systemLocaleFromOS(); loc != "" {
		return loc
	}
	return localeFromEnv()
}

func localeFromEnv() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if loc := usableEnvLocale(os.Getenv(key)); loc != "" {
			return loc
		}
	}
	return ""
}

func usableEnvLocale(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base, _, _ := strings.Cut(value, ".")
	base, _, _ = strings.Cut(base, "@")
	switch strings.ToUpper(base) {
	case "C", "POSIX":
		return ""
	}
	return value
}
