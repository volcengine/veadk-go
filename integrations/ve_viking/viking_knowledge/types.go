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

package viking_knowledge

type CollectionSearchKnowledgeRequest struct {
	Name           string          `json:"name,omitempty"`
	Project        string          `json:"project,omitempty"`
	ResourceId     string          `json:"resource_id,omitempty"`
	Query          string          `json:"query"`
	Limit          int32           `json:"limit"`
	QueryParam     *QueryParamInfo `json:"query_param"`
	DenseWeight    float32         `json:"dense_weight"`
	MdSearch       bool            `json:"md_search"`
	Preprocessing  PreProcessing   `json:"pre_processing,omitempty"`
	Postprocessing PostProcessing  `json:"post_processing,omitempty"`
}

type QueryParamInfo struct {
	DocFilter interface{} `json:"doc_filter"`
}

type PreProcessing struct {
	NeedInstruction  bool           `json:"need_instruction"`
	Rewrite          bool           `json:"rewrite"`
	Messages         []MessageParam `json:"messages"`
	ReturnTokenUsage bool           `json:"return_token_usage"`
}

type PostProcessing struct {
	RerankSwitch        bool                   `json:"rerank_switch"`
	RerankModel         string                 `json:"rerank_model,omitempty"`
	RerankOnlyChunk     bool                   `json:"rerank_only_chunk"`
	RetrieveCount       int32                  `json:"retrieve_count"`
	EndpointID          string                 `json:"endpoint_id"`
	ChunkDiffusionCount int32                  `json:"chunk_diffusion_count"`
	ChunkGroup          bool                   `json:"chunk_group"`
	ChunkScoreAggrType  string                 `json:"chunk_score_aggr_type,omitempty"`
	ChunkExtraContent   map[string]interface{} `json:"chunk_extra_content"`
	GetAttachmentLink   bool                   `json:"get_attachment_link"`
}

type MessageParam struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type ChatCompletionMessageContent struct {
	StringValue *string
	ListValue   []*ChatCompletionMessageContentPart
}

type ChatCompletionMessageContentPart struct {
	Type     ChatCompletionMessageContentPartType `json:"type,omitempty"`
	Text     string                               `json:"text,omitempty"`
	ImageURL *ChatMessageImageURL                 `json:"image_url,omitempty"`
}

type ChatCompletionMessageContentPartType string

type ChatMessageImageURL struct {
	URL string `json:"url,omitempty"`
}

type CollectionSearchKnowledgeResponse struct {
	Code      int64                                  `json:"code"`
	Message   string                                 `json:"message,omitempty"`
	Data      *CollectionSearchKnowledgeResponseData `json:"data,omitempty"`
	RequestID string                                 `json:"request_id,omitempty"`
}
type CollectionSearchKnowledgeResponseData struct {
	CollectionName string                          `json:"collection_name"`
	Count          int32                           `json:"count"`
	RewriteQuery   string                          `json:"rewrite_query,omitempty"` // 改写后的query,如果是多轮对话的首次请求，该字段为空，表示不改写，从第二个问题开始进行改写
	TokenUsage     *TotalTokenUsage                `json:"token_usage,omitempty"`
	ResultList     []*CollectionSearchResponseItem `json:"result_list,omitempty"`
}

type TotalTokenUsage struct {
	EmbeddingUsage *ModelTokenUsage `json:"embedding_token_usage,omitempty"`
	RerankUsage    *int64           `json:"rerank_token_usage,omitempty"`
	LLMUsage       *ModelTokenUsage `json:"llm_token_usage,omitempty"`
	RewriteUsage   *ModelTokenUsage `json:"rewrite_token_usage,omitempty"`
}

type ModelTokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`     // 请求文本的分词数
	CompletionTokens int64 `json:"completion_tokens"` // 生成文本的分词数, 对话模型才有值, 其他模型都是0
	TotalTokens      int64 `json:"total_tokens"`      // PromptTokens + CompletionTokens
}

type CollectionSearchResponseItem struct {
	Id                  string                              `json:"id"`
	Content             string                              `json:"content"`
	MdContent           string                              `json:"md_content,omitempty"`
	Score               float64                             `json:"score"`
	PointId             string                              `json:"point_id"`
	OriginText          string                              `json:"origin_text,omitempty"`
	OriginalQuestion    string                              `json:"original_question,omitempty"`
	ChunkTitle          string                              `json:"chunk_title,omitempty"`
	ChunkId             int                                 `json:"chunk_id"`
	ProcessTime         int64                               `json:"process_time"`
	RerankScore         float64                             `json:"rerank_score,omitempty"`
	DocInfo             CollectionSearchResponseItemDocInfo `json:"doc_info,omitempty"`
	RecallPosition      int32                               `json:"recall_position"`
	RerankPosition      int32                               `json:"rerank_position,omitempty"`
	ChunkType           string                              `json:"chunk_type,omitempty"`
	ChunkSource         string                              `json:"chunk_source,omitempty"`
	UpdateTime          int64                               `json:"update_time"`
	ChunkAttachmentList []ChunkAttachment                   `json:"chunk_attachment,omitempty"`
	TableChunkFields    []PointTableChunkField              `json:"table_chunk_fields,omitempty"`
	OriginalCoordinate  *ChunkPositions                     `json:"original_coordinate,omitempty"`
}

type CollectionSearchResponseItemDocInfo struct {
	Docid      string `json:"doc_id"`
	DocName    string `json:"doc_name"`
	CreateTime int64  `json:"create_time"`
	DocType    string `json:"doc_type"`
	DocMeta    string `json:"doc_meta,omitempty"`
	Source     string `json:"source"`
	Title      string `json:"title,omitempty"`
	Url        string `json:"url,omitempty"`
}

type ChunkAttachment struct {
	UUID    string `json:"uuid,omitempty"`
	Caption string `json:"caption"`
	Type    string `json:"type"`
	Link    string `json:"link,omitempty"`
}

type PointTableChunkField struct {
	FieldName  string      `json:"field_name"`
	FieldValue interface{} `json:"field_value"`
}

type ChunkPositions struct {
	PageNo []int       `json:"page_no"`
	BBox   [][]float64 `json:"bbox"`
}
type DocumentListResponse struct {
	Code      int64                    `json:"code"`
	Message   string                   `json:"message,omitempty"`
	Data      DocumentListResponseData `json:"data,omitempty"`
	RequestID string                   `json:"request_id,omitempty"`
}

type DocumentListResponseData struct {
	DocList []map[string]any `json:"doc_list,omitempty"`
	Count   int32            `json:"count,omitempty"`
}

type ChunkListResponse struct {
	Code      int64                 `json:"code"`
	Message   string                `json:"message,omitempty"`
	Data      ChunkListResponseData `json:"data,omitempty"`
	RequestID string                `json:"request_id,omitempty"`
}

type ChunkListResponseData struct {
	PointList []map[string]any `json:"point_list,omitempty"`
	Count     int32            `json:"count,omitempty"`
}

type CollectionNameProjectRequest struct {
	Name    string `json:"name"`
	Project string `json:"project"`
}

type CollectionCreateRequest struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	Description string `json:"description"`
}

type DocumentAddRequest struct {
	CollectionName string `json:"collection_name"`
	Project        string `json:"project"`
	AddType        string `json:"add_type"`
	TosPath        string `json:"tos_path"`
}

type DocumentDeleteRequest struct {
	CollectionName string `json:"collection_name"`
	Project        string `json:"project"`
	DocID          string `json:"doc_id"`
}

type CommonListRequest struct {
	CollectionName string `json:"collection_name"`
	Project        string `json:"project"`
	Offset         int32  `json:"offset"`
	Limit          int32  `json:"limit"`
}
