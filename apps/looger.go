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

package apps

import (
	"log"
	"strings"

	"github.com/gorilla/mux"
)

func RoutesLog(router *mux.Router) {
	log.Println("========================================")
	log.Println("Registered API Routes:")
	log.Println("========================================")

	_ = router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		path, _ := route.GetPathTemplate()
		methods, _ := route.GetMethods()

		if path != "" {
			methodStr := "ANY"
			if len(methods) > 0 {
				methodStr = strings.Join(methods, ", ")
			}
			log.Printf("%-8s %s", methodStr, path)
		}
		return nil
	})

	log.Println("========================================")
}
