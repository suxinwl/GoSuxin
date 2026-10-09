package suxinvideo

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type row = map[string]any

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func all(ctx context.Context, sql string, args ...any) ([]row, error) {
	result, err := g.DB().GetAll(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return gconv.Maps(result), nil
}
func one(ctx context.Context, sql string, args ...any) (row, error) {
	result, err := g.DB().GetOne(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return gconv.Map(result), nil
}
func execSQL(ctx context.Context, sql string, args ...any) error {
	_, err := g.DB().Exec(ctx, sql, args...)
	return err
}
func setting(ctx context.Context, key, fallback string) string {
	result, err := one(ctx, "SELECT value FROM sx_config WHERE `key`=?", key)
	if err != nil || result == nil {
		return fallback
	}
	value := gconv.String(result["value"])
	if value == "" {
		return fallback
	}
	return value
}
func checkIdentifier(v string) error {
	if !identifier.MatchString(v) {
		return errors.New("无效字段名")
	}
	return nil
}
func cleanTheme(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if regexp.MustCompile(`^[a-z][a-z0-9_-]{1,59}$`).MatchString(v) {
		return v
	}
	return "suxinlite"
}
