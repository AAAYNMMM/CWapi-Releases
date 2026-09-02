package security

import (
	"errors"
	"strings"
)

type Profile string

const (
	ProfileSafe Profile = "safe"
	ProfileFull Profile = "full"
)

func ParseProfile(value string) (Profile, error) {
	switch Profile(strings.ToLower(strings.TrimSpace(value))) {
	case "", ProfileSafe:
		return ProfileSafe, nil
	case ProfileFull:
		return ProfileFull, nil
	default:
		return "", errors.New("PERMANENT_POLICY_DENIED: access profile is invalid")
	}
}

func IsFull(value string) bool {
	profile, err := ParseProfile(value)
	return err == nil && profile == ProfileFull
}
