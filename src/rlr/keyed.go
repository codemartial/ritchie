// This file is the dictionary runtime for key types whose language equality is not
// Go's.
package rlr

// Keyed is one entry of a projected dictionary.
type Keyed[K, V any] struct {
	Key K
	Val V
}

// EntryKeyed is the entry that stores v under the projected key p, retaining the key value of an entry already there.
func EntryKeyed[P comparable, K, V any](m map[P]Keyed[K, V], p P, k K, v V) Keyed[K, V] {
	if entry, found := m[p]; found {
		k = entry.Key
	}
	return Keyed[K, V]{Key: k, Val: v}
}

// StoreKeyed inserts or replaces the entry under the projected key p.
func StoreKeyed[P comparable, K, V any](m map[P]Keyed[K, V], p P, k K, v V) {
	m[p] = EntryKeyed(m, p, k, v)
}

// EqualKeyed reports whether two projected dictionaries hold equal keys whose values are equal under eq.
func EqualKeyed[P comparable, K, V any](a, b map[P]Keyed[K, V], eq func(V, V) bool) bool {
	return EqualDicts(a, b, func(x, y Keyed[K, V]) bool { return eq(x.Val, y.Val) })
}

// LookupKeyed is the value under the projected key p and whether the key is present.
func LookupKeyed[P comparable, K, V any](m map[P]Keyed[K, V], p P) (V, bool) {
	entry, found := m[p]
	return entry.Val, found
}

// DictOf assembles a projected dictionary from paired keys and values in written order.
func DictOf[P comparable, K, V any](project func(K) P, keys []K, vals []V) map[P]Keyed[K, V] {
	out := make(map[P]Keyed[K, V], len(keys))
	for i, k := range keys {
		StoreKeyed(out, project(k), k, vals[i])
	}
	return out
}

// GetKeyed is the independent copy of the value under the projected key p.
func GetKeyed[P comparable, K, V any](m map[P]Keyed[K, V], p P, clone func(V) V) Option[V] {
	entry, found := m[p]
	if !found {
		return None[V]()
	}
	return Some(clone(entry.Val))
}

// GetFlatKeyed is GetKeyed over a dictionary of nullable values.
func GetFlatKeyed[P comparable, K, V any](m map[P]Keyed[K, Option[V]], p P, clone func(Option[V]) Option[V]) Option[V] {
	entry, found := m[p]
	if !found {
		return None[V]()
	}
	return clone(entry.Val)
}

// KeysKeyed is a fresh dynamic array of the key values of m in host order.
func KeysKeyed[P comparable, K, V any](m map[P]Keyed[K, V]) []K {
	out := make([]K, 0, len(m))
	for _, entry := range m {
		out = append(out, entry.Key)
	}
	return out
}

// ValuesKeyed is a fresh dynamic array of independent copies of the values of m in host order.
func ValuesKeyed[P comparable, K, V any](m map[P]Keyed[K, V], clone func(V) V) []V {
	out := make([]V, 0, len(m))
	for _, entry := range m {
		out = append(out, clone(entry.Val))
	}
	return out
}

// PairsKeyed is a fresh array of the entries of m in host order.
func PairsKeyed[P comparable, K, V any](m map[P]Keyed[K, V]) []Keyed[K, V] {
	out := make([]Keyed[K, V], 0, len(m))
	for _, entry := range m {
		out = append(out, entry)
	}
	return out
}

// RemoveKeyed is a fresh dictionary holding independent copies of every entry of m other than the one under the projected key p.
func RemoveKeyed[P comparable, K, V any](m map[P]Keyed[K, V], p P, clone func(V) V) map[P]Keyed[K, V] {
	out := make(map[P]Keyed[K, V], len(m))
	for key, entry := range m {
		if key != p {
			out[key] = Keyed[K, V]{Key: entry.Key, Val: clone(entry.Val)}
		}
	}
	return out
}

// RemoveKeyedInPlace is m without the entry under the projection p, in the storage of m, which the caller reads no more.
func RemoveKeyedInPlace[P comparable, K, V any](m map[P]Keyed[K, V], p P) map[P]Keyed[K, V] {
	delete(m, p)
	return m
}

// ZipKeyed is a fresh projected dictionary pairing each key of ks with the value of vs at the same position in increasing index order.
func ZipKeyed[P comparable, K, V any](ks []K, vs []V, project func(K) P, clone func(V) V) (map[P]Keyed[K, V], Fault) {
	if len(ks) != len(vs) {
		return nil, LengthMismatch
	}
	out := make(map[P]Keyed[K, V], len(ks))
	for i, k := range ks {
		StoreKeyed(out, project(k), k, clone(vs[i]))
	}
	return out, NoFault
}

// UnzipKeyed is one pair of fresh dynamic arrays holding the key values of m and independent copies of its values.
func UnzipKeyed[P comparable, K, V any](m map[P]Keyed[K, V], clone func(V) V) ([]K, []V) {
	keys := make([]K, 0, len(m))
	values := make([]V, 0, len(m))
	for _, entry := range m {
		keys = append(keys, entry.Key)
		values = append(values, clone(entry.Val))
	}
	return keys, values
}

// CloneKeyed is an independent copy of a projected dictionary.
func CloneKeyed[P comparable, K, V any](m map[P]Keyed[K, V], clone func(V) V) map[P]Keyed[K, V] {
	out := make(map[P]Keyed[K, V], len(m))
	for key, entry := range m {
		out[key] = Keyed[K, V]{Key: entry.Key, Val: clone(entry.Val)}
	}
	return out
}

// EntriesKeyed is the display form of a projected dictionary.
func EntriesKeyed[P comparable, K, V any](m map[P]Keyed[K, V], embedKey func(K) any, embedValue func(V) any) DictValue {
	out := DictValue{Keys: make([]any, 0, len(m)), Vals: make([]any, 0, len(m))}
	for _, entry := range m {
		out.Keys = append(out.Keys, embedKey(entry.Key))
		out.Vals = append(out.Vals, embedValue(entry.Val))
	}
	return out
}
