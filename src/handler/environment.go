package handler

import (
	"errors"
	"strings"
)

const (
	EnvironmentProd    = "prod"
	EnvironmentNonProd = "non-prod"
)

var ErrInvalidEnvironment = errors.New("environment must be \"prod\" or \"non-prod\"")

// normalizeEnvironment defaults an unset environment to prod, so agents that
// predate the field keep being counted as production.
func normalizeEnvironment(env string) (string, error) {
	switch env = strings.ToLower(strings.TrimSpace(env)); env {
	case "", EnvironmentProd:
		return EnvironmentProd, nil
	case EnvironmentNonProd:
		return EnvironmentNonProd, nil
	default:
		return "", ErrInvalidEnvironment
	}
}
