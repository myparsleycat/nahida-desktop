package pepad

type addressRange struct {
	Start uint32
	Len   uint32
}

func newRange(start, length uint32) addressRange {
	return addressRange{Start: start, Len: length}
}

func rangeFromUsize(start, length int, context string) (addressRange, error) {
	if _, err := addUsize(start, length, context); err != nil {
		return addressRange{}, err
	}
	if start < 0 || length < 0 || start > int(^uint32(0)) || length > int(^uint32(0)) {
		return addressRange{}, overflow(context)
	}
	return addressRange{Start: uint32(start), Len: uint32(length)}, nil
}

func (r addressRange) end() uint32 {
	return r.Start + r.Len
}

func (r addressRange) endU64() uint64 {
	return uint64(r.Start) + uint64(r.Len)
}

func (r addressRange) contains(value uint32) bool {
	return uint64(value) >= uint64(r.Start) && uint64(value) < r.endU64()
}

func (r addressRange) overlaps(other addressRange) bool {
	return uint64(r.Start) < other.endU64() && uint64(other.Start) < r.endU64()
}

func rangeContainsAny(r addressRange, values []uint32) bool {
	for _, value := range values {
		if r.contains(value) {
			return true
		}
	}
	return false
}

func rangeOverlapsAny(r addressRange, ranges []addressRange) bool {
	for _, other := range ranges {
		if r.overlaps(other) {
			return true
		}
	}
	return false
}
