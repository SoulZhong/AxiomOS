package mcp

import "time"

type timeT = time.Time

func parseTime(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.Local)
}
