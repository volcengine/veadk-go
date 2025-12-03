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
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/volcengine/veadk-go/configs"
	gormlog "gorm.io/gorm/logger"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var gLogger *Logger

type Logger struct {
	logger    *zap.Logger
	level     zapcore.Level
	gormLevel gormlog.LogLevel
}

func init() {
	gLogger = NewLogger(-2)
}

func NewLogger(level zapcore.Level) *Logger {
	var err error
	if level < zapcore.DebugLevel {
		level, err = zapcore.ParseLevel(configs.GetGlobalConfig().LOGGING.Level)
		if err != nil {
			level = zapcore.InfoLevel
		}
	}

	// 配置日志格式（文本或 JSON）
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "LOGGING",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 输出到stdout
	writeSyncer := zapcore.AddSync(os.Stdout)

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		writeSyncer,
		level,
	)

	var gormLevel gormlog.LogLevel
	switch level {
	case zapcore.DebugLevel, zapcore.InfoLevel:
		gormLevel = gormlog.Info
	case zapcore.WarnLevel:
		gormLevel = gormlog.Warn
	case zapcore.ErrorLevel:
		gormLevel = gormlog.Error
	case zapcore.FatalLevel:
		gormLevel = gormlog.Silent
	default:
		gormLevel = gormlog.Info
	}

	return &Logger{
		logger:    zap.New(core, zap.AddCaller()),
		level:     level,
		gormLevel: gormLevel,
	}
}

func (l *Logger) LogMode(level gormlog.LogLevel) gormlog.Interface {
	var zapLevel zapcore.Level
	switch level {
	case gormlog.Info:
		zapLevel = zapcore.InfoLevel
	case gormlog.Warn:
		zapLevel = zapcore.WarnLevel
	case gormlog.Error:
		zapLevel = zapcore.ErrorLevel
	case gormlog.Silent:
		zapLevel = zapcore.FatalLevel
	default:
		return l
	}
	return NewLogger(zapLevel)
}

func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.gormLevel <= gormlog.Silent {
		return
	}

	elapsed := time.Since(begin)
	switch {
	case err != nil:
		sql, rows := fc()
		if rows == -1 {
			l.Error(ctx, err.Error(), "cost", float64(elapsed.Nanoseconds())/1e6, "sql", sql)
		} else {
			l.Error(ctx, err.Error(), "cost", float64(elapsed.Nanoseconds())/1e6, "rows", rows, "sql", sql)
		}
	case l.gormLevel == gormlog.Info:
		sql, rows := fc()
		if rows == -1 {
			l.Info(ctx, "gorm", "cost", float64(elapsed.Nanoseconds())/1e6, "sql", sql)
		} else {
			l.Info(ctx, "gorm", "cost", float64(elapsed.Nanoseconds())/1e6, "rows", rows, "sql", sql)
		}
	}
}

func (l *Logger) Debug(ctx context.Context, msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		l.logger.Debug(msg, append(fields, zap.Error(err))...)
		return
	}
	l.logger.Debug(msg, fields...)
}

func Debug(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLogger.logger.Debug(msg, append(fields, zap.Error(err))...)
		return
	}
	gLogger.logger.Debug(msg, fields...)
}

func (l *Logger) Info(ctx context.Context, msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		l.logger.Info(msg, append(fields, zap.Error(err))...)
		return
	}
	l.logger.Info(msg, fields...)
}

func Info(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLogger.logger.Info(msg, append(fields, zap.Error(err))...)
		return
	}
	gLogger.logger.Info(msg, fields...)
}

func (l *Logger) Warn(ctx context.Context, msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		l.logger.Warn(msg, append(fields, zap.Error(err))...)
		return
	}
	l.logger.Warn(msg, fields...)
}

func Warn(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLogger.logger.Warn(msg, append(fields, zap.Error(err))...)
		return
	}
	gLogger.logger.Warn(msg, fields...)
}

func (l *Logger) Error(ctx context.Context, msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		l.logger.Error(msg, append(fields, zap.Error(err))...)
		return
	}
	l.logger.Error(msg, fields...)
}

func Error(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLogger.logger.Error(msg, append(fields, zap.Error(err))...)
		return
	}
	gLogger.logger.Error(msg, fields...)
}

// toZapFields 将键值对参数转换为 []zap.Field
func toZapFields(args ...interface{}) ([]zapcore.Field, error) {
	var fields []zapcore.Field
	if len(args)%2 != 0 {
		return fields, errors.New("invalid number of arguments: must be even (key-value pairs)")
	}

	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			return fields, fmt.Errorf("argument at index %d is not a string key", i)
		}
		value := args[i+1]
		field := toZapField(key, value)
		fields = append(fields, field)
	}
	return fields, nil
}

// toZapField 根据 value 类型转换为对应的 zap.Field
func toZapField(key string, value interface{}) zapcore.Field {
	if value == nil {
		return zap.String(key, "nil")
	}

	val := reflect.ValueOf(value)
	// 解引用指针
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return zap.String(key, "nil")
		}
		val = val.Elem()
	}

	// 处理 error 类型（优先于 interface 类型）
	if err, ok := value.(error); ok {
		return zap.Error(err)
	}

	// 根据类型转换为对应 zap.Field
	switch val.Kind() {
	case reflect.String:
		return zap.String(key, val.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return zap.Int64(key, val.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return zap.Uint64(key, val.Uint())
	case reflect.Float32, reflect.Float64:
		return zap.Float64(key, val.Float())
	case reflect.Bool:
		return zap.Bool(key, val.Bool())
	case reflect.Slice, reflect.Array:
		return zap.Any(key, value) // 切片/数组用 Any 处理
	case reflect.Map:
		return zap.Any(key, value) // 映射用 Any 处理
	case reflect.Struct:
		return zap.Any(key, value) // 结构体用 Any 处理
	default:
		return zap.Any(key, value) // 其他类型默认用 Any
	}
}
