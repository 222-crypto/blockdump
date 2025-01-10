package error_handling

import (
	"sync"
)

type ISendErrorChanneler interface {
	SErrorChannel() chan<- error
}

type IReceiveErrorChanneler interface {
	RErrorChannel() <-chan error
}

type IErrorChanneler interface {
	ISendErrorChanneler
	IReceiveErrorChanneler
}

type ErrorChanneler struct {
	error_channel    chan error
	error_channel_mu sync.Mutex
}

func (self *ErrorChanneler) ErrorChannel() chan error {
	if self == nil {
		panic("ErrorChanneler: self is nil")
	}

	self.error_channel_mu.Lock()
	defer self.error_channel_mu.Unlock()

	// Reuse existing channel if we have one
	if self.error_channel != nil {
		return self.error_channel
	}

	// Only create a new channel if we don't have one yet
	self.error_channel = make(chan error, 0) // unbuffered
	return self.error_channel
}

func (self *ErrorChanneler) SetErrorChannel(error_channel chan error) {
	self.error_channel_mu.Lock()
	defer self.error_channel_mu.Unlock()
	self.error_channel = error_channel
}

func (self *ErrorChanneler) RErrorChannel() <-chan error {
	return self.ErrorChannel()
}

func (self *ErrorChanneler) SErrorChannel() chan<- error {
	return self.ErrorChannel()
}

func NewErrorChanneler() IErrorChanneler {
	return &ErrorChanneler{}
}
