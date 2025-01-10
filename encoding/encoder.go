package encoding

import (
	"encoding/hex"
	"fmt"
	"io"

	"github.com/222-crypto/blockdump/v2/error_handling"
)

type Encoder interface {
	error_handling.IReceiveErrorChanneler
	Encode() io.Reader
}

// WriteEncoder [with optional hex encoding] processes the Encoder's output
// and writes it to the provided writer, blocking until the entire stream is processed.
// This design allows for efficient streaming of data while maintaining backpressure
// and proper error handling.
//
// Parameters:
//   - w: The writer that will provide the data to be encoded
//   - do_binary: If true, writes raw binary output; if false, writes hex-encoded output
//   - encoder: The encoder that will process the data stream
//
// Returns:
//   - error: Any error encountered during the writing or encoding process
//
// WriteEncoder processes an Encoder's output and writes it, either as raw binary
// or hex-encoded, to the provided writer while maintaining proper error handling.
func WriteEncoder(w io.Writer, do_binary bool, encoder Encoder) error {
	// Get the encoded data reader from the encoder
	reader := encoder.Encode()
	if reader == nil {
		return fmt.Errorf("encoder returned nil reader")
	}

	// Create error handling channels
	doneCh := make(chan struct{})
	errorCh := make(chan error, 1)

	// Start a goroutine to monitor the encoder's error channel
	go func() {
		defer close(doneCh)
		// Check for encoding errors from the encoder
		if encErr := <-encoder.RErrorChannel(); encErr != nil {
			errorCh <- fmt.Errorf("encoder error: %w", encErr)
		}
	}()

	// If binary output is requested, copy directly to writer
	if do_binary {
		_, err := io.Copy(w, reader)
		if err != nil {
			return fmt.Errorf("failed to write binary data: %w", err)
		}
	} else {
		// For hex output, create a hex encoder that writes to the output
		hexWriter := hex.NewEncoder(w)
		_, err := io.Copy(hexWriter, reader)
		if err != nil {
			return fmt.Errorf("failed to write hex-encoded data: %w", err)
		}
	}

	// Wait for error monitoring to complete
	<-doneCh

	// Check if any errors were reported
	select {
	case err := <-errorCh:
		return err
	default:
		return nil
	}
}
