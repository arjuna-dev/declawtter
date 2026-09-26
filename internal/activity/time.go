package activity

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseActivityTime(value any) (time.Time, bool) {
	switch current := value.(type) {
	case string:
		value := strings.TrimSpace(current)
		if value == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			"2006-01-02 15:04:05.999999999Z07:00",
			"2006-01-02 15:04:05",
		} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.UTC(), true
			}
		}
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return timeFromUnix(number), true
		}
	case float64:
		return timeFromUnix(current), current != 0
	case int64:
		return timeFromUnix(float64(current)), current != 0
	case int:
		return timeFromUnix(float64(current)), current != 0
	}
	return time.Time{}, false
}

func timeFromUnix(value float64) time.Time {
	if value > 1e14 {
		return time.Unix(0, int64(value)*int64(time.Microsecond)).UTC()
	}
	if value > 1e11 {
		return time.Unix(0, int64(value)*int64(time.Millisecond)).UTC()
	}
	return time.Unix(int64(value), int64((value-float64(int64(value)))*1e9)).UTC()
}

func formatActivityTime(value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%s", value.Local().Format("2006-01-02 15:04"))
}
