package merge

import (
	"encoding/json"
	"strconv"
	"strings"
)

// sizeUnits maps the suffixes hostRequirements accepts for memory and storage.
var sizeUnits = map[string]float64{
	"":   1,
	"b":  1,
	"kb": 1 << 10,
	"mb": 1 << 20,
	"gb": 1 << 30,
	"tb": 1 << 40,
}

// toBytes normalises a hostRequirements value to a comparable magnitude.
// Plain numbers (cpus) compare as-is; size strings such as "8gb" are expanded.
func toBytes(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case float64:
		return t, true
	case int:
		return float64(t), true
	case string:
		return parseSize(t)
	}
	return 0, false
}

func parseSize(s string) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}

	i := len(s)
	for i > 0 {
		c := s[i-1]
		if (c >= '0' && c <= '9') || c == '.' {
			break
		}
		i--
	}
	num := strings.TrimSpace(s[:i])
	unit := strings.TrimSpace(s[i:])

	mult, ok := sizeUnits[unit]
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	return f * mult, true
}
