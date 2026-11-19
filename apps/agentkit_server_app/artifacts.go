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

package agentkit_server_app

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/server/adkrest/controllers"
)

func deleteArtifactVersionHandler(service artifact.Service) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		for _, name := range []string{"app_name", "user_id", "session_id", "artifact_name", "version"} {
			if vars[name] == "" {
				http.Error(writer, name+" parameter is required", http.StatusBadRequest)
				return
			}
		}

		version, err := strconv.ParseInt(vars["version"], 10, 64)
		// ADK treats Version=0 as deleting all versions, so this endpoint must
		// reject zero even though it is a valid integer.
		if err != nil || version <= 0 {
			http.Error(writer, "version parameter must be a positive int64 integer", http.StatusBadRequest)
			return
		}
		if service == nil {
			http.Error(writer, "artifact service is not configured", http.StatusInternalServerError)
			return
		}

		err = service.Delete(request.Context(), &artifact.DeleteRequest{
			AppName:   vars["app_name"],
			UserID:    vars["user_id"],
			SessionID: vars["session_id"],
			FileName:  vars["artifact_name"],
			Version:   version,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		controllers.EncodeJSONResponse(nil, http.StatusOK, writer)
	}
}
