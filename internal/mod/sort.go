package mod

import (
	"sort"
)

type runeCursor struct {
	values []rune
	index  int
}

type pendingRune struct {
	value rune
	set   bool
}

func (c *runeCursor) next() (rune, bool) {
	if c.index >= len(c.values) {
		return 0, false
	}
	value := c.values[c.index]
	c.index++
	return value, true
}

func nextRune(c *runeCursor, pending *pendingRune) (rune, bool) {
	if pending.set {
		value := pending.value
		pending.set = false
		return value, true
	}
	return c.next()
}

func consumeASCIIDigits(c *runeCursor, pending *pendingRune) int {
	count := 0
	for {
		value, ok := c.next()
		if !ok {
			return count
		}
		if isASCIIDigit(value) {
			count++
			continue
		}
		pending.value = value
		pending.set = true
		return count
	}
}

// naturalCompare mirrors alphanumeric-sort 1.5.5 compare_str, which backs
// Electron's native ordinary-mod scanner. It is deliberately case-sensitive
// and compares Unicode scalar values rather than lower-cased UTF-8 bytes.
func naturalCompare(a, b string) int {
	left := runeCursor{values: []rune(a)}
	right := runeCursor{values: []rune(b)}
	var leftPending, rightPending pendingRune
	lastIsNumber := false
	preAnswer := 0

	for {
		ca, ok := nextRune(&left, &leftPending)
		if !ok {
			if rightPending.set {
				rightPending.set = false
				return -1
			}
			if _, ok := right.next(); ok {
				return -1
			}
			return preAnswer
		}
		cb, ok := nextRune(&right, &rightPending)
		if !ok {
			return 1
		}

		if isASCIIDigit(ca) && isASCIIDigit(cb) {
			leftLength, rightLength := 1, 1
			leadingZeroDelta := 0
			for ca == '0' {
				leadingZeroDelta++
				next, exists := left.next()
				if !exists {
					leftLength = 0
					break
				}
				if isASCIIDigit(next) {
					ca = next
					continue
				}
				leftPending = pendingRune{value: next, set: true}
				leftLength = 0
				break
			}
			for cb == '0' {
				leadingZeroDelta--
				next, exists := right.next()
				if !exists {
					rightLength = 0
					break
				}
				if isASCIIDigit(next) {
					cb = next
					continue
				}
				rightPending = pendingRune{value: next, set: true}
				rightLength = 0
				break
			}

			ordering := 0
			switch {
			case leftLength == 0 && rightLength != 0:
				return -1
			case leftLength != 0 && rightLength == 0:
				return 1
			case leftLength != 0 && rightLength != 0:
				for {
					ordering = compareRunes(ca, cb)
					if ordering != 0 {
						leftLength += consumeASCIIDigits(&left, &leftPending)
						rightLength += consumeASCIIDigits(&right, &rightPending)
						if leftLength != rightLength {
							ordering = compareInts(leftLength, rightLength)
						}
						break
					}
					nextLeft, leftExists := left.next()
					if !leftExists {
						if _, rightExists := right.next(); rightExists {
							return -1
						}
						break
					}
					if isASCIIDigit(nextLeft) {
						nextRight, rightExists := right.next()
						if !rightExists || !isASCIIDigit(nextRight) {
							return 1
						}
						ca, cb = nextLeft, nextRight
						continue
					}
					rightDigits := consumeASCIIDigits(&right, &rightPending)
					leftPending = pendingRune{value: nextLeft, set: true}
					if rightDigits > 0 {
						return -1
					}
					break
				}
			}

			if ordering != 0 {
				return ordering
			}
			switch compareInts(leadingZeroDelta, 0) {
			case 0:
				lastIsNumber = true
			case 1:
				if preAnswer == 0 {
					preAnswer = 1
				}
			case -1:
				if preAnswer == 0 {
					preAnswer = -1
				}
			}
			continue
		}

		ordering := compareRunes(ca, cb)
		if ordering == 0 {
			lastIsNumber = false
			continue
		}
		if lastIsNumber && ((ca > 255) != (cb > 255)) {
			return -ordering
		}
		return ordering
	}
}

func naturalLess(a, b string) bool { return naturalCompare(a, b) < 0 }

func sortLocaleStrings(values []string) {
	less := newLocaleLess()
	sort.SliceStable(values, func(i, j int) bool { return less(values[i], values[j]) })
}

func isASCIIDigit(value rune) bool { return value >= '0' && value <= '9' }

func compareRunes(a, b rune) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareInts(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
