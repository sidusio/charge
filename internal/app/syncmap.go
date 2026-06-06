package app

import "sync"

type SyncMap[K comparable, V any] struct {
	m sync.Map
}

func (m *SyncMap[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	return v.(V), ok
}

func (m *SyncMap[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}
