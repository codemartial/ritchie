// This file is the dictionary runtime, which enumerates a dictionary in the
// unspecified order of a Go map traversal.
package rlr

// DictLength is the entry count of a dictionary as an Int.
func DictLength[K comparable, V any](m map[K]V) int64 {
	return int64(len(m))
}

// Has reports whether k is a key of m.
func Has[K comparable, V any](m map[K]V, k K) bool {
	_, found := m[k]
	return found
}

// CheckKey validates that k is a key of m.
func CheckKey[K comparable, V any](m map[K]V, k K) Fault {
	if _, found := m[k]; !found {
		return MissingKey
	}
	return NoFault
}

// Get is the independent copy of the value stored under k.
func Get[K comparable, V any](m map[K]V, k K, clone func(V) V) Option[V] {
	v, found := m[k]
	if !found {
		return None[V]()
	}
	return Some(clone(v))
}

// GetFlat is Get over a dictionary of nullable values.
func GetFlat[K comparable, V any](m map[K]Option[V], k K, clone func(Option[V]) Option[V]) Option[V] {
	v, found := m[k]
	if !found {
		return None[V]()
	}
	return clone(v)
}

// Keys is a fresh dynamic array of the keys of m in unspecified order.
func Keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Values is a fresh dynamic array of independent copies of the values of m in unspecified order, which need not be the order of a separate Keys call.
func Values[K comparable, V any](m map[K]V, clone func(V) V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, clone(v))
	}
	return out
}

// Remove is a fresh dictionary holding independent copies of every entry of m other than the one under k.
func Remove[K comparable, V any](m map[K]V, k K, clone func(V) V) map[K]V {
	out := make(map[K]V, len(m))
	for key, v := range m {
		if key != k {
			out[key] = clone(v)
		}
	}
	return out
}

// RemoveInPlace is m without the entry under k, in the storage of m, which the caller reads no more.
func RemoveInPlace[K comparable, V any](m map[K]V, k K) map[K]V {
	delete(m, k)
	return m
}

// Zip is a fresh dictionary pairing each key of ks with the value of vs at the same position.
func Zip[K comparable, V any](ks []K, vs []V, clone func(V) V) (map[K]V, Fault) {
	if len(ks) != len(vs) {
		return nil, LengthMismatch
	}
	out := make(map[K]V, len(ks))
	for i, k := range ks {
		out[k] = clone(vs[i])
	}
	return out, NoFault
}

// Unzip is one pair of fresh dynamic arrays holding the keys of m and independent copies of its values, filled by one traversal, so that the key and the value at one index belong to one entry.
func Unzip[K comparable, V any](m map[K]V, clone func(V) V) ([]K, []V) {
	keys := make([]K, 0, len(m))
	values := make([]V, 0, len(m))
	for k, v := range m {
		keys = append(keys, k)
		values = append(values, clone(v))
	}
	return keys, values
}

// CloneDict is an independent copy of a dictionary.
func CloneDict[K comparable, V any](m map[K]V, clone func(V) V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = clone(v)
	}
	return out
}

// EqualDicts reports whether two dictionaries hold the same keys with values equal under eq.
func EqualDicts[K comparable, V any](a, b map[K]V, eq func(V, V) bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, found := b[k]
		if !found || !eq(v, w) {
			return false
		}
	}
	return true
}
