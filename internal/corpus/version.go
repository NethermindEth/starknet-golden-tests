package corpus

import (
	"fmt"
	"strings"
)

// MajorMinor returns the major.minor part of a spec version such as "0.10.3".
func MajorMinor(version string) (string, error) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("spec version %q is not major.minor[.patch]", version)
	}
	return parts[0] + "." + parts[1], nil
}
