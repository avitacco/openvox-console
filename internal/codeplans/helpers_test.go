package codeplans

import "errors"

func asErr(err error, target any) bool { return errors.As(err, target) }
