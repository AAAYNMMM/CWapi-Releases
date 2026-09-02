//go:build !windows

package invocation

import (
	"errors"

	"github.com/AAAYNMMM/CWapi/internal/processcontract"
)

func New([]string) (*Resolver, error) {
	return nil, errors.New("INVOCATION_PLATFORM_UNSUPPORTED")
}

func (r *Resolver) Resolve(string, processcontract.StartArguments, ...string) (Final, error) {
	return Final{}, errors.New("INVOCATION_PLATFORM_UNSUPPORTED")
}
