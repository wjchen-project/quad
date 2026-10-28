package api

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

func validateUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if username == "" {
		return "", errors.New("用户名不能为空")
	}
	if !usernamePattern.MatchString(username) {
		return "", errors.New("用户名需为 3-32 位字母、数字或 _ . -")
	}
	return username, nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return errors.New("密码至少 8 位")
	}
	if len(password) > 128 {
		return errors.New("密码最多 128 位")
	}
	return nil
}

func validateTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	n := utf8.RuneCountInString(title)
	if n == 0 {
		return "", errors.New("待办标题不能为空")
	}
	if n > 200 {
		return "", errors.New("待办标题最多 200 字")
	}
	return title, nil
}

func validateNotes(notes string) error {
	if utf8.RuneCountInString(notes) > 4000 {
		return errors.New("备注最多 4000 字")
	}
	return nil
}

func validateProgress(progress int) error {
	if progress < 0 || progress > 100 {
		return errors.New("进度必须在 0-100 之间")
	}
	return nil
}

// timeParamLayouts 是查询参数接受的时间格式，没有时区信息时按 UTC 解析。
var timeParamLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
}

// parseTimeParam 解析时间区间边界。纯日期默认取当天 00:00:00，
// endOfDay 为 true 时取当天 23:59:59.999，便于 to 参数包含完整一天。
func parseTimeParam(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range timeParamLayouts {
		if ts, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return ts.UTC(), nil
		}
	}
	ts, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		return time.Time{}, errors.New("时间格式无效，支持 2006-01-02、2006-01-02 15:04 或 RFC3339")
	}
	if endOfDay {
		ts = ts.AddDate(0, 0, 1).Add(-time.Millisecond)
	}
	return ts.UTC(), nil
}
