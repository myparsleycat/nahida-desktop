package ui

import (
	"math"
	"strconv"
	"strings"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// parseInt reads a whole number typed into a field, allowing surrounding spaces.
func parseInt(text string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	return n, err == nil
}

// formatSize writes a byte count in binary units with up to two decimals, as the React screens did.
func formatSize(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(n)
	unit := 0
	for math.Abs(value) >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64) + " " + units[unit]
}

// formatDateTime writes t as date-fns did in the React screens: withTime gives the short date and time ("Pp"),
// otherwise the medium date with seconds ("PPpp"). Japanese used the English formats there too.
func formatDateTime(t time.Time, withTime bool) string {
	lang := catalog.Language()
	switch {
	case strings.HasPrefix(lang, "ko"):
		meridiem := "오전"
		if t.Hour() >= 12 {
			meridiem = "오후"
		}
		if withTime {
			return t.Format("2006.01.02") + " " + meridiem + " " + t.Format("3:04")
		}
		return t.Format("2006년 1월 2일") + " " + meridiem + " " + t.Format("3:04:05")
	case strings.HasPrefix(lang, "zh"):
		meridiem := "上午"
		if t.Hour() >= 12 {
			meridiem = "下午"
		}
		if withTime {
			return t.Format("06-01-02") + " " + meridiem + " " + t.Format("3:04")
		}
		return t.Format("2006-01-02") + " " + meridiem + " " + t.Format("3:04:05")
	}
	if withTime {
		return t.Format("01/02/2006, 3:04 PM")
	}
	return t.Format("Jan 2, 2006, 3:04:05 PM")
}
