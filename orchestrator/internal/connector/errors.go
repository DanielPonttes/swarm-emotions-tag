package connector

import (
	"errors"
	"fmt"
)

var ErrDependencyUnavailable = errors.New("dependency_unavailable")

type DependencyUnavailableError struct {
	Dependency string
	Cause      error
}

func (e *DependencyUnavailableError) Error() string {
	if e == nil {
		return ErrDependencyUnavailable.Error()
	}
	if e.Dependency == "" {
		return ErrDependencyUnavailable.Error()
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", ErrDependencyUnavailable.Error(), e.Dependency)
	}
	return fmt.Sprintf("%s: %s: %v", ErrDependencyUnavailable.Error(), e.Dependency, e.Cause)
}

func (e *DependencyUnavailableError) Unwrap() error {
	return ErrDependencyUnavailable
}

func IsDependencyUnavailable(err error) bool {
	return errors.Is(err, ErrDependencyUnavailable)
}
