package main

import (
	"fmt"
	"time"
)

// commaInt formats an integer with comma thousand-separators. e.g. 1234567 → "1,234,567"
func commaInt(n int) string {
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+(len(s)-1)/3)
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// formatRowCount formats a row count with comma separators and a short-form label in brackets.
// e.g. 1234567 → "1,234,567 (1.2 Million)"
func formatRowCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+(len(s)-1)/3)
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	commaSep := string(out)

	shortLabel := func(v float64, unit string) string {
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f %s", v, unit)
		}
		return fmt.Sprintf("%.1f %s", v, unit)
	}

	var short string
	switch {
	case n >= 1_000_000_000_000_000:
		short = shortLabel(float64(n)/1_000_000_000_000_000, "Quadrillion")
	case n >= 1_000_000_000_000:
		short = shortLabel(float64(n)/1_000_000_000_000, "Trillion")
	case n >= 1_000_000_000:
		short = shortLabel(float64(n)/1_000_000_000, "Billion")
	case n >= 1_000_000:
		short = shortLabel(float64(n)/1_000_000, "Million")
	case n >= 1_000:
		v := float64(n) / 1_000
		if v == float64(int64(v)) {
			short = fmt.Sprintf("%.0fK", v)
		} else {
			short = fmt.Sprintf("%.1fK", v)
		}
	}

	if short == "" {
		return commaSep
	}
	return commaSep + " (" + short + ")"
}

// formatDuration converts a duration to a human-readable string such as
// "5 seconds", "2 minutes 30 seconds", "1 hour 15 minutes", etc.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	part := func(n int, singular, plural string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", singular)
		}
		return fmt.Sprintf("%d %s", n, plural)
	}

	switch {
	case h > 0 && m > 0 && s > 0:
		return part(h, "hour", "hours") + " " + part(m, "minute", "minutes") + " " + part(s, "second", "seconds")
	case h > 0 && m > 0:
		return part(h, "hour", "hours") + " " + part(m, "minute", "minutes")
	case h > 0 && s > 0:
		return part(h, "hour", "hours") + " " + part(s, "second", "seconds")
	case h > 0:
		return part(h, "hour", "hours")
	case m > 0 && s > 0:
		return part(m, "minute", "minutes") + " " + part(s, "second", "seconds")
	case m > 0:
		return part(m, "minute", "minutes")
	default:
		return part(s, "second", "seconds")
	}
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
