package shell

import "errors"

func execErrorAs(err error, target interface{}) bool {
	return errors.As(err, target)
}
