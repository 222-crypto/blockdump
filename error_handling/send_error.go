package error_handling

// SendError attempts to send the error to the channel. If the channel is not ready to receive,
// it passes the error to the provided error handling function.
func SendError(errCh chan<- error, fallback func(error), err error) {
	select {
	case errCh <- err:
		// Successfully sent the error to the channel
	default:
		// Channel is not ready to receive, handle the error
		fallback(err)
	}
}
