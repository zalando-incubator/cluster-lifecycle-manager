package util

import "sync"

func Contains[T comparable](s []T, e T) bool {
	for _, v := range s {
		if v == e {
			return true
		}
	}
	return false
}

type LazyOf[T any] struct {
	New   func() (T, error)
	once  sync.Once
	value T
}

func (l *LazyOf[T]) Value() (T, error) {
	var err error
	if l.New != nil {
		l.once.Do(func() {
			l.value, err = l.New()
			l.New = nil
		})
	}
	return l.value, err
}

func NewLazyOf[T any](newfunc func() (T, error)) *LazyOf[T] {
	return &LazyOf[T]{New: newfunc}
}
