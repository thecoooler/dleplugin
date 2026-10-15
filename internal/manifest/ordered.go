package manifest

import (
	"bytes"
	"encoding/json"
)

// OrderedMap is a string-keyed map that preserves insertion order.
type OrderedMap[V any] struct {
	keys   []string
	values map[string]V
}

// NewOrderedMap returns an empty map.
func NewOrderedMap[V any]() *OrderedMap[V] {
	return &OrderedMap[V]{values: map[string]V{}}
}

// Set inserts or replaces a value; new keys go to the end.
func (m *OrderedMap[V]) Set(key string, value V) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Get returns the value for key.
func (m *OrderedMap[V]) Get(key string) (V, bool) {
	v, ok := m.values[key]
	return v, ok
}

// Keys returns keys in insertion order.
func (m *OrderedMap[V]) Keys() []string {
	return append([]string(nil), m.keys...)
}

// Len returns the number of entries.
func (m *OrderedMap[V]) Len() int {
	return len(m.keys)
}

// Clone returns a shallow copy of the map.
func (m *OrderedMap[V]) Clone() *OrderedMap[V] {
	c := NewOrderedMap[V]()
	for _, k := range m.keys {
		c.Set(k, m.values[k])
	}
	return c
}

// MarshalJSON encodes the map as a JSON object in insertion order.
func (m *OrderedMap[V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
