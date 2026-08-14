package httpapi

import "strings"

const (
	sharedPlanWebPrefix = "/shared/plans/"
	sharedPlanAPIPrefix = "/api/v1/shared/plans/"
)

// sensitiveRequestLabel redacts anonymous share token and asset-ref path segments
// into fixed route templates so access/error/panic logs never retain raw secrets.
// Non-sensitive paths are returned unchanged.
func sensitiveRequestLabel(path string) string {
	switch {
	case strings.HasPrefix(path, sharedPlanAPIPrefix):
		return redactSharedPlanPath(sharedPlanAPIPrefix, path)
	case strings.HasPrefix(path, sharedPlanWebPrefix):
		return redactSharedPlanPath(sharedPlanWebPrefix, path)
	default:
		return path
	}
}

func redactSharedPlanPath(prefix, path string) string {
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return path
	}
	token, after, found := strings.Cut(rest, "/")
	if token == "" {
		return path
	}
	label := prefix + ":token"
	if !found {
		return label
	}
	return label + "/" + redactSharedAssetSegments(after)
}

func redactSharedAssetSegments(path string) string {
	parts := strings.Split(path, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "assets" && parts[i+1] != "" {
			parts[i+1] = ":ref"
		}
	}
	return strings.Join(parts, "/")
}
