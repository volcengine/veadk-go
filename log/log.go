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
	"errors"
	"fmt"
	"os"
	"reflect"

	"veadk-go/configs"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var gLOGGING *zap.Logger

func init() {
	SetupLog()
}

func SetupLog() {
	level := configs.GetGlobalConfig().LOGGING.Level
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		zapLevel = zapcore.InfoLevel
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

	// 构建核心组件：编码器、输出目标、日志级别（Debug）
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		writeSyncer,
		zapLevel,
	)

	gLOGGING = zap.New(core, zap.AddCaller())
}

func Debug(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLOGGING.Debug(msg, append(fields, zap.Error(err))...)
		return
	}
	gLOGGING.Debug(msg, fields...)
}

func Info(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLOGGING.Info(msg, append(fields, zap.Error(err))...)
		return
	}
	gLOGGING.Info(msg, fields...)
}

func Warn(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLOGGING.Warn(msg, append(fields, zap.Error(err))...)
		return
	}
	gLOGGING.Warn(msg, fields...)
}

func Error(msg string, args ...interface{}) {
	fields, err := toZapFields(args...)
	if err != nil {
		// 若参数解析出错，添加错误信息并继续输出日志
		gLOGGING.Error(msg, append(fields, zap.Error(err))...)
		return
	}
	gLOGGING.Error(msg, fields...)
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
