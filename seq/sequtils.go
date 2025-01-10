package seq

import (
	"iter"
)

// :: cannot range over seq (variable of type iter.Seq[T])
// apparently, we can't range over seq, so this function
// converts it to a channel, which we can range over
func ChanSeq[T any](seq iter.Seq[T]) <-chan T {
	ch := make(chan T)
	go func() {
		seq(func(item T) bool {
			ch <- item
			return true
		})

		close(ch)
	}()
	return ch
}

// similar to ChanSeq, but for Seq2
func ChanSeq2[O any, T any](seq iter.Seq2[O, T]) <-chan struct {
	One O
	Two T
} {
	ch := make(chan struct {
		One O
		Two T
	})
	go func() {
		defer close(ch)
		seq(func(one O, two T) bool {
			ch <- struct {
				One O
				Two T
			}{One: one, Two: two}
			return true
		})
	}()
	return ch
}

// identical to ChanSeq2, with different struct nomenclature
func ChanSeq2KV[K any, V any](seq iter.Seq2[K, V]) <-chan struct {
	Key K
	Val V
} {
	ch := make(chan struct {
		Key K
		Val V
	})
	go func() {
		defer close(ch)
		seq(func(key K, val V) bool {
			ch <- struct {
				Key K
				Val V
			}{Key: key, Val: val}
			return true
		})
	}()
	return ch
}

// similar to ChanSeq, but for Seq2[T, error]
func ChanSeq2Results[T any](seq iter.Seq2[T, error]) <-chan struct {
	Val T
	Err error
} {
	ch := make(chan struct {
		Val T
		Err error
	})
	go func() {
		defer close(ch)
		seq(func(val T, err error) bool {
			ch <- struct {
				Val T
				Err error
			}{Val: val, Err: err}
			return true
		})
	}()
	return ch
}
