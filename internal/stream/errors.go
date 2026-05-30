package stream

import "errors"

// Stream manager errors.
var (
	// ErrMaxClientsReached is returned when the maximum number of concurrent
	// clients has been reached.
	ErrMaxClientsReached = errors.New("maximum number of concurrent clients reached")
)
