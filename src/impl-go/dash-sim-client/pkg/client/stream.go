package client

import (
	"errors"
	"io"
)

// errEOF is kept distinct to allow comparison via errors.Is in older code
// paths; in practice io.EOF is what gRPC streams return on completion.
var errEOF = errors.New("EOF")

func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, errEOF)
}
