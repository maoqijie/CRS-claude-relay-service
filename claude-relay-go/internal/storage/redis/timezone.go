package redis

import (
	"fmt"
	"time"

	"github.com/catstream/claude-relay-go/internal/config"
)

// DefaultTimezoneOffset 默认时区偏移量（UTC+8，与 Node.js 实现保持一致）
const DefaultTimezoneOffset = 8

// getTimezoneOffset 获取配置的时区偏移量
func getTimezoneOffset() time.Duration {
	if config.Cfg != nil && config.Cfg.System.TimezoneOffset != 0 {
		return time.Duration(config.Cfg.System.TimezoneOffset) * time.Hour
	}
	return time.Duration(DefaultTimezoneOffset) * time.Hour
}

// getDateInTimezone 获取指定时区的日期
func getDateInTimezone(t time.Time) time.Time {
	return t.UTC().Add(getTimezoneOffset())
}

// getDateStringInTimezone 获取指定时区的日期字符串 (YYYY-MM-DD)
func getDateStringInTimezone(t time.Time) string {
	tz := getDateInTimezone(t)
	return tz.Format("2006-01-02")
}

// getHourInTimezone 获取指定时区的小时数
func getHourInTimezone(t time.Time) int {
	tz := getDateInTimezone(t)
	return tz.Hour()
}

// getHourStringInTimezone 获取指定时区的小时字符串 (YYYY-MM-DD:HH)
func getHourStringInTimezone(t time.Time) string {
	tz := getDateInTimezone(t)
	return fmt.Sprintf("%s:%02d", tz.Format("2006-01-02"), tz.Hour())
}

// getMonthStringInTimezone 获取指定时区的月份字符串 (YYYY-MM)
func getMonthStringInTimezone(t time.Time) string {
	tz := getDateInTimezone(t)
	return tz.Format("2006-01")
}

// getWeekStringInTimezone 获取指定时区的周字符串 (YYYY-WXX)
func getWeekStringInTimezone(t time.Time) string {
	tz := getDateInTimezone(t)
	year, week := tz.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// getMinuteTimestamp 获取分钟级时间戳
func getMinuteTimestamp(t time.Time) int64 {
	return t.Unix() / 60 * 60
}

// getCurrentDateString 获取当前日期字符串
func getCurrentDateString() string {
	return getDateStringInTimezone(time.Now())
}

// getCurrentMonthString 获取当前月份字符串
func getCurrentMonthString() string {
	return getMonthStringInTimezone(time.Now())
}

// getCurrentHourString 获取当前小时字符串
func getCurrentHourString() string {
	return getHourStringInTimezone(time.Now())
}

// parseDateString 解析日期字符串
func parseDateString(dateStr string) (time.Time, error) {
	return time.Parse("2006-01-02", dateStr)
}

// parseMonthString 解析月份字符串
func parseMonthString(monthStr string) (time.Time, error) {
	return time.Parse("2006-01", monthStr)
}

// getDaysInRange 获取日期范围内的所有日期字符串
func getDaysInRange(start, end time.Time) []string {
	var days []string
	current := start
	for !current.After(end) {
		days = append(days, getDateStringInTimezone(current))
		current = current.AddDate(0, 0, 1)
	}
	return days
}

// getMonthsInRange 获取月份范围内的所有月份字符串
func getMonthsInRange(start, end time.Time) []string {
	var months []string
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !current.After(endMonth) {
		months = append(months, getMonthStringInTimezone(current))
		current = current.AddDate(0, 1, 0)
	}
	return months
}
