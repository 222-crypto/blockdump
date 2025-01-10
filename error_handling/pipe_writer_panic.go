package error_handling

import (
	"fmt"
	"io"
)

// PipeWriterPanic provides a robust error handling mechanism for pipe-based operations by
// ensuring errors are properly propagated through both a channel and the pipe itself. It
// allows for flexible handling of errors through an optional panic mechanism.
//
// When an error occurs in pipe writing operations, this function ensures the error is
// communicated in two ways:
//  1. Through an error channel for wider error awareness
//  2. Through the pipe writer's CloseWithError to notify the reading end
//
// The function uses SendError as its foundation, which attempts a non-blocking send of the
// error through the channel. If the channel cannot immediately receive the error (e.g., if
// it's full or has no receiver), SendError will execute a fallback that ensures the pipe
// reader is notified and optionally triggers a panic.
//
// Parameters:
//
//   - do_panic: A boolean flag that controls the severity of the error handling. When true,
//     the function will panic after ensuring error propagation. When false, it allows for
//     continued execution after error handling. This flexibility lets callers adjust the
//     response based on error severity.
//
//   - pipe_writer: A pointer to an io.PipeWriter representing the writing end of a pipe.
//     The function ensures any goroutine reading from the corresponding PipeReader will
//     be notified of the error through CloseWithError.
//
//   - errors: A write-only error channel (chan<- error) used as the primary mechanism for
//     error propagation throughout the system. This channel should typically be buffered
//     to reduce the chance of blocking.
//
//   - err: The error that triggered this error handling. This same error will be propagated
//     through both the channel and the pipe writer.
//
// The function guarantees that errors will be handled appropriately regardless of the
// channel's state, making it particularly useful in cleanup scenarios or when error
// handling must not block. It combines immediate local error handling (closing the pipe)
// with broader system error propagation (via the channel).
func PipeWriterPanic(do_panic bool, pipe_writer *io.PipeWriter, errors chan<- error, err error) {
	if pipe_writer == nil {
		panic(fmt.Errorf("nil pipe writer"))
	}

	if errors == nil {
		panic(fmt.Errorf("nil error channel"))
	}

	SendError(errors, func(err error) {
		// Ensure reader sees error
		pipe_writer.CloseWithError(err)
		if do_panic {
			panic(err)
		}
	}, err)
}
