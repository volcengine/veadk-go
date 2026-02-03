// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package log

import (
	"context"
	"fmt"
	ilog "log"
	"log/slog"
	"os"
	"strings"
	"time"

	gormlog "gorm.io/gorm/logger"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
)

func init() {
	levelStr := utils.GetEnvWithDefault(common.LOGGING_LEVEL, configs.GetGlobalConfig().LOGGING.Level, common.DEFAULT_LOGGING_LEVER)

	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(levelStr))); err != nil {
		slog.Warn(fmt.Sprintf("config log level '%s' not recognized, defaulting to INFO", levelStr))
		level = slog.LevelInfo
	}
	logger := NewLogger(level)

	slog.SetDefault(logger)
}

func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format("2006-01-02 15:04:05.000000"))
				}
			}
			return a
		},
	}))
}

func Printf(format string, v ...any) {
	ilog.Printf(format, v...)
}

func Println(v ...any) {
	ilog.Println(v...)
}

func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

func Debugf(format string, v ...any) {
	slog.Debug(fmt.Sprintf(format, v...))
}

func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

func Infof(format string, v ...any) {
	slog.Info(fmt.Sprintf(format, v...))
}

func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

func Warnf(format string, v ...any) {
	slog.Warn(fmt.Sprintf(format, v...))
}

func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

func Errorf(format string, v ...any) {
	slog.Error(fmt.Sprintf(format, v...))
}

//func Fatal(v ...any) {
//	ilog.Fatal(v...)
//}
//
//func Fatalf(format string, v ...any) {
//	ilog.Fatalf(format, v...)
//}

type GormLogger struct {
	logger *slog.Logger
}

func NewGormLogger(level slog.Level) *GormLogger {
	return &GormLogger{
		logger: NewLogger(level),
	}
}

func (g *GormLogger) LogMode(level gormlog.LogLevel) gormlog.Interface {
	var slogLevel slog.Level
	switch level {
	case gormlog.Info:
		slogLevel = slog.LevelInfo
	case gormlog.Warn:
		slogLevel = slog.LevelWarn
	case gormlog.Error, gormlog.Silent:
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	g.logger = NewLogger(slogLevel)

	return g
}

func (g *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []any{
		slog.String("sql", sql),
		slog.Duration("elapsed", elapsed),
		slog.Int64("rows", rows),
	}

	if err != nil {
		fields = append(fields, slog.String("error", err.Error()))
		g.logger.WarnContext(ctx, "gorm trace", fields...)
		return
	}

	g.logger.InfoContext(ctx, "gorm trace", fields...)
}

func (g *GormLogger) Debug(ctx context.Context, msg string, args ...interface{}) {
	g.logger.DebugContext(ctx, msg, args...)
}

func (g *GormLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	g.logger.InfoContext(ctx, msg, args...)
}

func (g *GormLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	g.logger.WarnContext(ctx, msg, args...)
}

func (g *GormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	g.logger.ErrorContext(ctx, msg, args...)
}
