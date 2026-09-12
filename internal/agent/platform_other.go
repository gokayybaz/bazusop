//go:build !linux && !windows

package agent

import "errors"

func platformFacts() (PlatformFacts, error) {
	return PlatformFacts{}, errors.New("bazusop-agent supports Linux and Windows")
}
