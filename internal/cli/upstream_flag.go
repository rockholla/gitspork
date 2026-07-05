package cli

import (
	"fmt"
	"strings"

	"github.com/rockholla/gitspork/internal/types"
)

// ParseUpstreamFlag parses a comma-separated key=value --upstream flag value.
// Valid keys: url (required), version, subpath, token.
func ParseUpstreamFlag(val string) (types.UpstreamSpec, error) {
	spec := types.UpstreamSpec{}
	for _, part := range strings.Split(val, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return spec, fmt.Errorf("--upstream: invalid key=value pair %q", part)
		}
		switch kv[0] {
		case "url":
			spec.URL = kv[1]
		case "version":
			spec.Version = kv[1]
		case "subpath":
			spec.Subpath = kv[1]
		case "token":
			spec.Token = kv[1]
		default:
			return spec, fmt.Errorf("--upstream: unknown key %q", kv[0])
		}
	}
	if spec.URL == "" {
		return spec, fmt.Errorf("--upstream: missing required key \"url\"")
	}
	return spec, nil
}
