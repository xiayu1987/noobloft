// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

package localization

import (
	"fmt"
	"strings"
)

type Error struct {
	Key  string
	Args []any
}

type Localizer interface {
	Message(language string) string
}

func Errorf(key string, args ...any) *Error { return &Error{Key: key, Args: args} }

func (e *Error) Error() string { return e.Message("en") }

func (e *Error) Message(language string) string {
	args := make([]any, len(e.Args))
	for i, arg := range e.Args {
		if err, ok := arg.(error); ok {
			args[i] = Localize(err, language)
		} else {
			args[i] = arg
		}
	}
	template, ok := lookup(language, e.Key)
	if !ok {
		if len(args) == 0 {
			return e.Key
		}
		parts := make([]string, len(args))
		for i, arg := range args {
			parts[i] = fmt.Sprint(arg)
		}
		return e.Key + ": " + strings.Join(parts, ", ")
	}
	return fmt.Sprintf(template, args...)
}

func (e *Error) Unwrap() []error {
	var errs []error
	for _, arg := range e.Args {
		if err, ok := arg.(error); ok {
			errs = append(errs, err)
		}
	}
	return errs
}

func Localize(err error, language string) string {
	if err == nil {
		return ""
	}
	if localized, ok := err.(Localizer); ok {
		return localized.Message(language)
	}
	return err.Error()
}
