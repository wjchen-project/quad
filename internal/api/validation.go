package api

import (
	"errors"
	"regexp"
	"strings"
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
