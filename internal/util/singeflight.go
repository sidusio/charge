package util

import "golang.org/x/sync/singleflight"

type SingleFlightGroup[T any] struct {
	internal singleflight.Group
}

func (g *SingleFlightGroup[T]) Do(key string, fn func() (T, error)) (T, error, bool) {
	res, err, shared := g.internal.Do(key, func() (any, error) {
		res, err := fn()
		return res, err
	})
	return res.(T), err, shared
}
