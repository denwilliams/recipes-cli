// Package fault defines actionable CLI errors shared by the implementation layers.
package fault

import "fmt"

type Error struct{ Code, Message string }

func (e *Error) Error() string                   { return e.Message }
func New(code, format string, args ...any) error { return &Error{code, fmt.Sprintf(format, args...)} }
