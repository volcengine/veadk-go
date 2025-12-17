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

package tool

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func GetCityWeather(city string) (map[string]any, error) {
	fixedWeather := map[string]struct {
		condition   string
		temperature int
	}{
		"beijing":   {"Sunny", 25},
		"shanghai":  {"Cloudy", 22},
		"guangzhou": {"Rainy", 28},
		"shenzhen":  {"Partly cloudy", 29},
		"chengdu":   {"Windy", 20},
		"hangzhou":  {"Snowy", -2},
		"wuhan":     {"Humid", 26},
		"chongqing": {"Hazy", 30},
		"xi'an":     {"Cool", 18},
		"nanjing":   {"Hot", 32},
	}
	c := strings.ToLower(strings.TrimSpace(city))
	if info, ok := fixedWeather[c]; ok {
		return map[string]any{"result": fmt.Sprintf("%s, %d°C", info.condition, info.temperature)}, nil
	}
	return nil, fmt.Errorf("weather information not found for %s", c)
}

type GetCityWeatherArgs struct {
	City string `json:"city" jsonschema:"The target city name which must be in English"`
}

func GetCityWeatherTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, args GetCityWeatherArgs) (map[string]any, error) {
		return GetCityWeather(args.City)
	}
	return functiontool.New(
		functiontool.Config{
			Name: "get_city_weather",
			Description: `A tools for querying real-time weather information.
Args:
	city: The target city name.
Returns:
	the weather of the target city.`,
		},
		handler)
}
