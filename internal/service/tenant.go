//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package service

import (
	"context"
	"fmt"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine"
	"ragflow/internal/entity"
	"ragflow/internal/server"
	"strings"
)

// TenantService tenant service
type TenantService struct {
	tenantDAO            *dao.TenantDAO
	userTenantDAO        *dao.UserTenantDAO
	modelProviderDAO     *dao.TenantModelProviderDAO
	modelInstanceDAO     *dao.TenantModelInstanceDAO
	modelDAO             *dao.TenantModelDAO
	modelGroupDAO        *dao.TenantModelGroupDAO
	modelGroupMappingDAO *dao.TenantModelGroupMappingDAO
	kbDAO                *dao.KnowledgebaseDAO
	docEngine            engine.DocEngine
}

// NewTenantService create tenant service
func NewTenantService() *TenantService {
	return &TenantService{
		tenantDAO:            dao.NewTenantDAO(),
		userTenantDAO:        dao.NewUserTenantDAO(),
		modelProviderDAO:     dao.NewTenantModelProviderDAO(),
		modelInstanceDAO:     dao.NewTenantModelInstanceDAO(),
		modelDAO:             dao.NewTenantModelDAO(),
		modelGroupDAO:        dao.NewTenantModelGroupDAO(),
		modelGroupMappingDAO: dao.NewTenantModelGroupMappingDAO(),
		kbDAO:                dao.NewKnowledgebaseDAO(),
		docEngine:            engine.Get(),
	}
}

// TenantInfoResponse tenant information response
type TenantInfoResponse struct {
	TenantID  string  `json:"tenant_id"`
	Name      *string `json:"name,omitempty"`
	LLMID     string  `json:"llm_id"`
	EmbDID    string  `json:"embd_id"`
	RerankID  string  `json:"rerank_id"`
	ASRID     string  `json:"asr_id"`
	Img2TxtID string  `json:"img2txt_id"`
	TTSID     *string `json:"tts_id,omitempty"`
	ParserIDs string  `json:"parser_ids"`
	Role      string  `json:"role"`
}

// GetTenantInfo get tenant information for the current user (owner tenant)
func (s *TenantService) GetTenantInfo(userID string) (*TenantInfoResponse, error) {
	tenantInfos, err := s.tenantDAO.GetInfoByUserID(userID)
	if err != nil {
		return nil, err
	}
	if len(tenantInfos) == 0 {
		return nil, nil // No tenant found (should not happen for valid user)
	}
	// Return the first tenant (should be only one owner tenant per user)
	ti := tenantInfos[0]
	return &TenantInfoResponse{
		TenantID:  ti.TenantID,
		Name:      ti.Name,
		LLMID:     ti.LLMID,
		EmbDID:    ti.EmbDID,
		RerankID:  ti.RerankID,
		ASRID:     ti.ASRID,
		Img2TxtID: ti.Img2TxtID,
		TTSID:     ti.TTSID,
		ParserIDs: ti.ParserIDs,
		Role:      ti.Role,
	}, nil
}

// TenantListItem tenant list item response
type TenantListItem struct {
	TenantID     string  `json:"tenant_id"`
	Role         string  `json:"role"`
	Nickname     string  `json:"nickname"`
	Email        string  `json:"email"`
	Avatar       string  `json:"avatar"`
	UpdateDate   string  `json:"update_date"`
	DeltaSeconds float64 `json:"delta_seconds"`
}

// TenantLLMService tenant LLM service
// This service handles operations related to tenant-specific LLM configurations
type TenantLLMService struct {
	tenantLLMDAO *dao.TenantLLMDAO
}

// NewTenantLLMService creates a new TenantLLMService instance
func NewTenantLLMService() *TenantLLMService {
	return &TenantLLMService{
		tenantLLMDAO: dao.NewTenantLLMDAO(),
	}
}

// GetAPIKey retrieves the tenant LLM record by tenant ID and model name
/**
 * This method splits the model name into name and factory parts using the "@" separator,
 * then queries the database for the matching tenant LLM configuration.
 *
 * Parameters:
 *   - tenantID: the unique identifier of the tenant
 *   - modelName: the model name, optionally including factory suffix (e.g., "gpt-4@OpenAI")
 *
 * Returns:
 *   - *model.TenantLLM: the tenant LLM record if found, nil otherwise
 *   - error: an error if the query fails, nil otherwise
 *
 * Example:
 *
 *	service := NewTenantLLMService()
 *
 *	// Get API key for model with factory
 *	tenantLLM, err := service.GetAPIKey("tenant-123", "gpt-4@OpenAI")
 *	if err != nil {
 *	    log.Printf("Error: %v", err)
 *	}
 *
 *	// Get API key for model without factory
 *	tenantLLM, err := service.GetAPIKey("tenant-123", "gpt-4")
 */
func (s *TenantLLMService) GetAPIKey(tenantID, modelName string) (*entity.TenantLLM, error) {
	modelName, factory := s.SplitModelNameAndFactory(modelName)

	var tenantLLM *entity.TenantLLM
	var err error

	if factory == "" {
		tenantLLM, err = s.tenantLLMDAO.GetByTenantIDAndLLMName(tenantID, modelName)
	} else {
		tenantLLM, err = s.tenantLLMDAO.GetByTenantIDLLMNameAndFactory(tenantID, modelName, factory)
	}

	if err != nil {
		return nil, err
	}

	return tenantLLM, nil
}

// SplitModelNameAndFactory splits a model name into name and factory parts
func (s *TenantLLMService) SplitModelNameAndFactory(modelName string) (string, string) {
	arr := strings.Split(modelName, "@")
	if len(arr) < 2 {
		return modelName, ""
	}
	if len(arr) > 2 {
		return strings.Join(arr[0:len(arr)-1], "@"), arr[len(arr)-1]
	}
	return arr[0], arr[1]
}

// EnsureTenantModelIDForParams ensures tenant model IDs are populated for LLM-related parameters
/**
 * This method iterates through a predefined list of LLM-related parameter keys (llm_id, embd_id,
 * asr_id, img2txt_id, rerank_id, tts_id) and automatically populates the corresponding tenant_*
 * fields (tenant_llm_id, tenant_embd_id, etc.) with the tenant LLM record IDs.
 *
 * If a parameter key exists and its corresponding tenant_* key doesn't exist, this method will:
 *  1. Query the tenant LLM record using GetAPIKey
 *  2. If found, set the tenant_* key to the record's ID
 *  3. If not found, set the tenant_* key to 0
 *
 * Parameters:
 *   - tenantID: the unique identifier of the tenant
 *   - params: a map of parameters to be updated (will be modified in place)
 *
 * Returns:
 *   - map[string]interface{}: the updated parameters map (same as input, modified in place)
 *
 * Example:
 *
 *	service := NewTenantLLMService()
 *	params := map[string]interface{}{
 *	    "llm_id": "gpt-4@OpenAI",
 *	    "embd_id": "text-embedding-3-small@OpenAI",
 *	}
 *	result := service.EnsureTenantModelIDForParams("tenant-123", params)
 *	// result will contain:
 *	// {
 *	//     "llm_id": "gpt-4@OpenAI",
 *	//     "embd_id": "text-embedding-3-small@OpenAI",
 *	//     "tenant_llm_id": 123,    // ID from tenant_llm table
 *	//     "tenant_embd_id": 456,   // ID from tenant_llm table
 *	// }
 */
func (s *TenantLLMService) EnsureTenantModelIDForParams(tenantID string, params map[string]interface{}) map[string]interface{} {
	paramKeys := []string{"llm_id", "embd_id", "asr_id", "img2txt_id", "rerank_id", "tts_id"}

	for _, key := range paramKeys {
		tenantKey := "tenant_" + key

		if value, exists := params[key]; exists && value != nil && value != "" {
			if _, tenantExists := params[tenantKey]; !tenantExists {
				modelName, ok := value.(string)
				if !ok || modelName == "" {
					continue
				}

				tenantLLM, err := s.GetAPIKey(tenantID, modelName)
				if err == nil && tenantLLM != nil {
					params[tenantKey] = tenantLLM.ID
				} else {
					params[tenantKey] = int64(0)
				}
			}
		}
	}

	return params
}

// GetTenantList get tenant list for a user
func (s *TenantService) GetTenantList(userID string) ([]*TenantListItem, error) {
	tenants, err := s.userTenantDAO.GetTenantsByUserID(userID)
	if err != nil {
		return nil, err
	}

	result := make([]*TenantListItem, len(tenants))

	for i, t := range tenants {
		// Parse update_date and calculate delta_seconds
		var deltaSeconds float64
		if t.UpdateDate != "" {
			deltaSeconds, err = common.DeltaSeconds(t.UpdateDate)
			if err != nil {
				return nil, err
			}
		}

		result[i] = &TenantListItem{
			TenantID:     t.TenantID,
			Role:         t.Role,
			Nickname:     t.Nickname,
			Email:        t.Email,
			Avatar:       t.Avatar,
			UpdateDate:   t.UpdateDate,
			DeltaSeconds: deltaSeconds,
		}
	}

	return result, nil
}

// CreateMetadataStore creates the metadata store for a tenant
func (s *TenantService) CreateMetadataStore(tenantID string) (common.ErrorCode, error) {
	// Call document engine to create doc meta table
	err := s.docEngine.CreateMetadataStore(context.Background(), tenantID)
	if err != nil {
		return common.CodeServerError, fmt.Errorf("failed to create metadata table: %w", err)
	}

	return common.CodeSuccess, nil
}

// DeleteMetadataStore deletes the metadata store for a tenant
func (s *TenantService) DeleteMetadataStore(tenantID string) (common.ErrorCode, error) {
	// Call document engine to delete doc meta table
	err := s.docEngine.DropMetadataStore(context.Background(), tenantID)
	if err != nil {
		return common.CodeServerError, fmt.Errorf("failed to delete doc meta table: %w", err)
	}

	return common.CodeSuccess, nil
}

// CreateDatasetTableRequest represents the request for creating a dataset table
type CreateDatasetTableRequest struct {
	KBID       string `json:"kb_id" binding:"required"`
	VectorSize int    `json:"vector_size" binding:"required"`
	ParserID   string `json:"parser_id,omitempty"`
}

// CreateChunkStoreResponse represents the response for creating a chunk store
type CreateChunkStoreResponse struct {
	KBID       string `json:"kb_id"`
	TableName  string `json:"table_name"`
	VectorSize int    `json:"vector_size"`
}

// CreateChunkStore creates a chunk store in the document engine for a knowledge base
func (s *TenantService) CreateChunkStore(req *CreateDatasetTableRequest) (*CreateChunkStoreResponse, common.ErrorCode, error) {
	if req == nil {
		return nil, common.CodeDataError, fmt.Errorf("request is required")
	}
	// Get KB to find tenant_id for building table name
	kb, err := s.kbDAO.GetByID(req.KBID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return nil, common.CodeDataError, fmt.Errorf("knowledge base not found: %s", req.KBID)
		}
		return nil, common.CodeServerError, fmt.Errorf("failed to query knowledge base %s: %w", req.KBID, err)
	}

	// vector_size is required
	vecSize := req.VectorSize
	if vecSize <= 0 {
		return nil, common.CodeDataError, fmt.Errorf("vector_size must be positive")
	}

	// Build table name prefix: ragflow_<tenant_id>
	tableName := fmt.Sprintf("ragflow_%s", kb.TenantID)

	// Call document engine to create table
	// Full table name will be built as "{tableName}_{kb_id}"
	err = s.docEngine.CreateChunkStore(context.Background(), tableName, req.KBID, vecSize, req.ParserID)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("failed to create dataset: %w", err)
	}

	return &CreateChunkStoreResponse{
		KBID:       req.KBID,
		TableName:  tableName,
		VectorSize: vecSize,
	}, common.CodeSuccess, nil
}

// DeleteChunkStore deletes the chunk store in the document engine for a knowledge base
func (s *TenantService) DeleteChunkStore(kbID string) (common.ErrorCode, error) {
	// Get KB to find tenant_id for building table name
	kb, err := s.kbDAO.GetByID(kbID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return common.CodeDataError, fmt.Errorf("knowledge base not found: %s", kbID)
		}
		return common.CodeServerError, fmt.Errorf("failed to query knowledge base %s: %w", kbID, err)
	}

	// Call document engine to delete table
	err = s.docEngine.DropChunkStore(context.Background(), fmt.Sprintf("ragflow_%s", kb.TenantID), kbID)
	if err != nil {
		return common.CodeServerError, fmt.Errorf("failed to delete table: %w", err)
	}

	return common.CodeSuccess, nil
}

type ModelItem struct {
	ModelProvider *string `json:"model_provider"`
	ModelInstance *string `json:"model_instance"`
	ModelName     *string `json:"model_name"`
	ModelType     string  `json:"model_type"`
	Enable        bool    `json:"enable"`
}

type DefaultModelResponse struct {
	Models []ModelItem `json:"models,omitempty"`
}

// GetDefaultModelName returns the full default model ID for a tenant and model type
// Format: modelName@instanceName@providerName or modelName@providerName
// Returns empty string if no default model is set
func (s *TenantService) GetDefaultModelName(tenantID string, modelType entity.ModelType) (string, error) {
	tenant, err := s.tenantDAO.GetByID(tenantID)
	if err != nil {
		return "", err
	}

	var modelID string
	switch modelType {
	case entity.ModelTypeChat:
		modelID = tenant.LLMID
	case entity.ModelTypeEmbedding:
		modelID = tenant.EmbdID
	case entity.ModelTypeRerank:
		modelID = tenant.RerankID
	case entity.ModelTypeSpeech2Text:
		modelID = tenant.ASRID
	case entity.ModelTypeImage2Text:
		modelID = tenant.Img2TxtID
	case entity.ModelTypeTTS:
		modelID = *tenant.TTSID
	case entity.ModelTypeOCR:
		modelID = tenant.OCRID
	default:
		return "", fmt.Errorf("invalid model type: %s", modelType)
	}

	return modelID, nil
}

// MODEL_TAG_TO_TYPE maps model type tags to standard model type names
// This matches Python's MODEL_TAG_TO_TYPE in models_api_service.py
var MODEL_TAG_TO_TYPE = map[string]string{
	"chat":      "chat",
	"embedding": "embedding",
	"rerank":    "rerank",
	"asr":       "speech2text",
	"vision":    "image2text",
	"tts":       "tts",
	"ocr":       "ocr",
}

func (s *TenantService) GetModelInfo(tenantID string, defaultModel string, modelType string) (*string, *string, *string, string, bool, error) {
	// normally the model string is: modelName@instanceName@providerName, sometimes it's just modelName@providerName
	// for the 1st case, parse defaultChatModel into three parts
	defaultChatModelParts := strings.Split(defaultModel, "@")
	var providerName *string
	var instanceName *string
	var modelName *string
	if len(defaultChatModelParts) == 3 {
		providerName = &defaultChatModelParts[2]
		instanceName = &defaultChatModelParts[1]
		modelName = &defaultChatModelParts[0]

	} else if len(defaultChatModelParts) == 2 {
		providerName = &defaultChatModelParts[1]
		instanceName = new(string)
		*instanceName = "default"
		modelName = &defaultChatModelParts[0]
	} else {
		return nil, nil, nil, "", false, fmt.Errorf("invalid model string: %s", defaultModel)
	}

	// Convert model type tag to standard model type name (matches Python's MODEL_TAG_TO_TYPE)
	mappedModelType, ok := MODEL_TAG_TO_TYPE[modelType]
	if !ok {
		mappedModelType = modelType
	}

	if mappedModelType == "ocr" {
		if *providerName == "infiniflow" && *instanceName == "default" && *modelName == "deepdoc" {
			return providerName, instanceName, modelName, mappedModelType, true, nil
		}
	}

	// Check if the provider and instance exists
	modelProvider, err := s.modelProviderDAO.GetByTenantIDAndProviderName(tenantID, *providerName)
	if err != nil {
		return nil, nil, nil, "", false, err
	}

	modelInstance, err := s.modelInstanceDAO.GetByProviderIDAndInstanceName(modelProvider.ID, *instanceName)
	if err != nil {
		return nil, nil, nil, "", false, err
	}

	modelSchema, err := dao.GetModelProviderManager().GetModelByName(*providerName, *modelName)
	if err == nil && !modelSchema.ModelTypeMap[mappedModelType] {
		return nil, nil, nil, "", false, fmt.Errorf("model %s isn't a %s model", *modelName, mappedModelType)
	}

	var modelEntity *entity.TenantModel
	modelEntity, err = s.modelDAO.GetModelByProviderIDAndInstanceIDAndModelTypeAndModelName(modelProvider.ID, modelInstance.ID, mappedModelType, *modelName)
	if err != nil {
		errString := err.Error()
		if !strings.Contains(errString, "record not found") {
			return nil, nil, nil, "", false, err
		}
	}

	// enable = true if:
	// 1. modelEntity is nil (no record exists), OR
	// 2. modelEntity exists but status is NOT "inactive"
	enable := modelEntity == nil || modelEntity.Status != "inactive"

	return providerName, instanceName, modelName, mappedModelType, enable, nil

}

func (s *TenantService) ListTenantDefaultModels(userID string) ([]ModelItem, error) {

	tenantInfos, err := s.tenantDAO.GetInfoByUserID(userID)
	if err != nil {
		return nil, err
	}
	if len(tenantInfos) == 0 {
		return nil, nil // No tenant found (should not happen for valid user)
	}

	ownedTenant := tenantInfos[0]

	var result []ModelItem

	defaultChatModelProvider, defaultChatModelInstance, defaultChatModelName, defaultChatModelType, defaultChatModelEnable, err := s.GetModelInfo(ownedTenant.TenantID, ownedTenant.LLMID, "chat")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultChatModelProvider,
			ModelInstance: defaultChatModelInstance,
			ModelName:     defaultChatModelName,
			ModelType:     defaultChatModelType,
			Enable:        defaultChatModelEnable,
		})
	}

	defaultEmbeddingModelProvider, defaultEmbeddingModelInstance, defaultEmbeddingModelName, defaultEmbeddingModelType, defaultEmbeddingModelEnable, err := s.GetModelInfo(ownedTenant.TenantID, ownedTenant.EmbDID, "embedding")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultEmbeddingModelProvider,
			ModelInstance: defaultEmbeddingModelInstance,
			ModelName:     defaultEmbeddingModelName,
			ModelType:     defaultEmbeddingModelType,
			Enable:        defaultEmbeddingModelEnable,
		})
	}

	defaultRerankModelProvider, defaultRerankModelInstance, defaultRerankModelName, defaultRerankModelType, defaultRerankModelEnable, err := s.GetModelInfo(ownedTenant.TenantID, ownedTenant.RerankID, "rerank")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultRerankModelProvider,
			ModelInstance: defaultRerankModelInstance,
			ModelName:     defaultRerankModelName,
			ModelType:     defaultRerankModelType,
			Enable:        defaultRerankModelEnable,
		})
	}

	defaultASRModelProvider, defaultASRModelInstance, defaultASRModelName, defaultASRModelType, defaultASREnable, err := s.GetModelInfo(ownedTenant.TenantID, ownedTenant.ASRID, "asr")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultASRModelProvider,
			ModelInstance: defaultASRModelInstance,
			ModelName:     defaultASRModelName,
			ModelType:     defaultASRModelType,
			Enable:        defaultASREnable,
		})
	}

	defaultImage2TextModelProvider, defaultImage2TextModelInstance, defaultImage2TextModelName, defaultImage2TextModelType, defaultImage2TextModelEnable, err := s.GetModelInfo(ownedTenant.TenantID, ownedTenant.Img2TxtID, "vision")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultImage2TextModelProvider,
			ModelInstance: defaultImage2TextModelInstance,
			ModelName:     defaultImage2TextModelName,
			ModelType:     defaultImage2TextModelType,
			Enable:        defaultImage2TextModelEnable,
		})
	}

	defaultOCRModelProvider, defaultOCRModelInstance, defaultOCRModelName, defaultOCRModelType, defaultOCRModelEnable, err := s.GetModelInfo(ownedTenant.TenantID, ownedTenant.OCRID, "ocr")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultOCRModelProvider,
			ModelInstance: defaultOCRModelInstance,
			ModelName:     defaultOCRModelName,
			ModelType:     defaultOCRModelType,
			Enable:        defaultOCRModelEnable,
		})
	}

	if ownedTenant.TTSID == nil {
		return result, nil
	}

	defaultTTSModelProvider, defaultTTSModelInstance, defaultTTSModelName, defaultTTSModelType, defaultTTSModelEnable, err := s.GetModelInfo(ownedTenant.TenantID, *ownedTenant.TTSID, "tts")
	if err == nil {
		result = append(result, ModelItem{
			ModelProvider: defaultTTSModelProvider,
			ModelInstance: defaultTTSModelInstance,
			ModelName:     defaultTTSModelName,
			ModelType:     defaultTTSModelType,
			Enable:        defaultTTSModelEnable,
		})
	}

	return result, nil
}

func (s *TenantService) checkModelAvailable(tenantID, providerName, instanceName, modelName, modelType string) error {
	// Check if the provider and instance exists
	modelProvider, err := s.modelProviderDAO.GetByTenantIDAndProviderName(tenantID, providerName)
	if err != nil {
		return err
	}

	modelInstance, err := s.modelInstanceDAO.GetByProviderIDAndInstanceName(modelProvider.ID, instanceName)
	if err != nil {
		return err
	}

	modelSchema, err := dao.GetModelProviderManager().GetModelByName(providerName, modelName)
	if err != nil {
		return err
	}

	if !modelSchema.ModelTypeMap[modelType] {
		return fmt.Errorf("model %s isn't a chat model", modelName)
	}

	var modelEntity *entity.TenantModel
	modelEntity, err = s.modelDAO.GetModelByProviderIDAndInstanceIDAndModelTypeAndModelName(modelProvider.ID, modelInstance.ID, modelType, modelName)
	if err != nil || modelEntity != nil {
		var errString = err.Error()
		if errString == "record not found" {
			return nil
		}
		return fmt.Errorf("model %s isn't available", modelName)
	}

	return nil
}

func (s *TenantService) SetTenantDefaultModels(userID, modelProvider, modelInstance, modelName, modelType string) error {

	tenantInfos, err := s.tenantDAO.GetInfoByUserID(userID)
	if err != nil {
		return err
	}
	if len(tenantInfos) == 0 {
		return nil // No tenant found (should not happen for valid user)
	}

	ownedTenant := tenantInfos[0]
	var defaultModel string
	var modelTypeID string
	if modelType == "chat" {
		modelTypeID = "llm_id"
	}
	if modelType == "embedding" {
		modelTypeID = "embd_id"
	}
	if modelType == "rerank" {
		modelTypeID = "rerank_id"
	}
	if modelType == "asr" {
		modelTypeID = "asr_id"
	}
	if modelType == "vision" {
		modelTypeID = "img2txt_id"
	}
	if modelType == "tts" {
		modelTypeID = "tts_id"
	}
	if modelType == "ocr" {
		modelTypeID = "ocr_id"
	}
	if modelTypeID == "" {
		return fmt.Errorf("model type %s is invalid", modelType)
	}

	if modelProvider == "" && modelInstance == "" && modelName == "" {
		defaultModel = ""
	} else if modelProvider != "" && modelInstance != "" && modelName != "" {
		err = s.checkModelAvailable(ownedTenant.TenantID, modelProvider, modelInstance, modelName, modelType)
		if err != nil {
			return err
		}
		defaultModel = fmt.Sprintf("%s@%s@%s", modelName, modelInstance, modelProvider)
	} else {
		return fmt.Errorf("model provider, instance and name must be specified together")
	}

	err = s.tenantDAO.Update(ownedTenant.TenantID, map[string]interface{}{
		modelTypeID: defaultModel,
	})

	return nil
}

// AddedModelItem represents a model in the list of added models
type AddedModelItem struct {
	ModelType    []string `json:"model_type"`
	Name         string   `json:"name"`
	ProviderID   string   `json:"provider_id"`
	ProviderName string   `json:"provider_name"`
	InstanceID   string   `json:"instance_id"`
	InstanceName string   `json:"instance_name"`
}

// ListTenantAddedModels lists all added models for a tenant
// This implements the Python models_api_service.list_tenant_added_models function
func (s *TenantService) ListTenantAddedModels(tenantID string, modelTypeFilter string) ([]AddedModelItem, error) {
	// Step 1: Verify tenant exists
	tenant, err := s.tenantDAO.GetByID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant not found")
	}
	if tenant == nil {
		return nil, fmt.Errorf("tenant not found")
	}

	// Step 2: Normalize model type filter (lowercase if provided)
	if modelTypeFilter != "" {
		modelTypeFilter = strings.ToLower(modelTypeFilter)
	}

	// Step 3: Get all providers for tenant
	providers, err := s.modelProviderDAO.GetByTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return []AddedModelItem{}, nil
	}

	// Step 4: Get all instances for those providers
	providerIDs := make([]string, len(providers))
	providerInfoMap := make(map[string]*entity.TenantModelProvider)
	for i, p := range providers {
		providerIDs[i] = p.ID
		providerInfoMap[p.ID] = p
	}

	instances, err := s.modelInstanceDAO.GetByProviderIDs(providerIDs)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return []AddedModelItem{}, nil
	}

	// Step 5: Build provider_instance_map: map[provider_name][]instance
	providerInstanceMap := make(map[string][]*entity.TenantModelInstance)
	for _, inst := range instances {
		providerName := ""
		if p, ok := providerInfoMap[inst.ProviderID]; ok {
			providerName = p.ProviderName
		}
		providerInstanceMap[providerName] = append(providerInstanceMap[providerName], inst)
	}

	// Step 6: Get all model records
	instanceIDs := make([]string, len(instances))
	instanceInfoMap := make(map[string]*entity.TenantModelInstance)
	for i, inst := range instances {
		instanceIDs[i] = inst.ID
		instanceInfoMap[inst.ID] = inst
	}

	modelRecords, err := s.modelDAO.GetModelsByProviderIDsAndInstanceIDs(providerIDs, instanceIDs)
	if err != nil {
		return nil, err
	}

	// Step 7: Filter by model_type if provided and build model_record_map
	modelRecordMap := make(map[string][]*entity.TenantModel)
	for _, model := range modelRecords {
		if modelTypeFilter != "" && model.ModelType != modelTypeFilter {
			continue
		}
		key := fmt.Sprintf("%s_%s_%s", model.ProviderID, model.InstanceID, model.ModelName)
		modelRecordMap[key] = append(modelRecordMap[key], model)
	}

	// Step 8: Build provider_names list for factory matching
	providerNames := make([]string, len(providers))
	for i, p := range providers {
		providerNames[i] = p.ProviderName
	}

	var addedModels []AddedModelItem
	modelKeyInFactory := make(map[string]bool)

	// Step 9: Iterate through factory providers
	factories := server.GetModelProviders()
	for _, factory := range factories {
		// Check if this factory is in our tenant's providers
		found := false
		for _, pn := range providerNames {
			if pn == factory.Name {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		factoryInstances, ok := providerInstanceMap[factory.Name]
		if !ok || len(factoryInstances) == 0 {
			continue
		}

		// Step 10: Iterate through each LLM in the factory
		for _, llm := range factory.LLMs {
			// Apply model type filter
			if modelTypeFilter != "" && llm.ModelType != modelTypeFilter {
				continue
			}

			// Step 11: For each factory instance, check model records
			for _, factoryInstance := range factoryInstances {
				modelRecordKey := fmt.Sprintf("%s_%s_%s", factoryInstance.ProviderID, factoryInstance.ID, llm.LLMName)
				modelKeyInFactory[modelRecordKey] = true

				manualModifiedModels := modelRecordMap[modelRecordKey]

				// Determine active and inactive model types
				var activeModelTypes []string
				var inactiveModelTypes []string
				for _, manualModel := range manualModifiedModels {
					if manualModel.Status == "inactive" {
						inactiveModelTypes = append(inactiveModelTypes, manualModel.ModelType)
					} else {
						activeModelTypes = append(activeModelTypes, manualModel.ModelType)
					}
				}

				// Calculate final model_types: (set([llm["model_type"]] + active_model_types) - set(inactive_model_types))
				modelTypesSet := make(map[string]bool)
				modelTypesSet[llm.ModelType] = true
				for _, t := range activeModelTypes {
					modelTypesSet[t] = true
				}
				for _, t := range inactiveModelTypes {
					delete(modelTypesSet, t)
				}

				if len(modelTypesSet) == 0 {
					continue
				}

				var modelTypes []string
				for t := range modelTypesSet {
					modelTypes = append(modelTypes, t)
				}

				providerName := ""
				if p, ok := providerInfoMap[factoryInstance.ProviderID]; ok {
					providerName = p.ProviderName
				}

				addedModels = append(addedModels, AddedModelItem{
					ModelType:    modelTypes,
					Name:         llm.LLMName,
					ProviderID:   factoryInstance.ProviderID,
					ProviderName: providerName,
					InstanceID:   factoryInstance.ID,
					InstanceName: factoryInstance.InstanceName,
				})
			}
		}
	}

	// Step 12: Handle manual_added_models (models in tenant_model but not in factory)
	for modelRecordKey, modelRecords := range modelRecordMap {
		if modelKeyInFactory[modelRecordKey] {
			continue
		}

		if len(modelRecords) == 0 {
			continue
		}

		// Parse key: provider_id_instance_id_model_name
		parts := strings.Split(modelRecordKey, "_")
		if len(parts) < 3 {
			continue
		}
		providerID := parts[0]
		instanceID := parts[1]
		modelName := strings.Join(parts[2:], "_") // model name might contain underscores

		// Get active model types
		var modelTypes []string
		for _, model := range modelRecords {
			if model.Status != "inactive" {
				modelTypes = append(modelTypes, model.ModelType)
			}
		}

		if len(modelTypes) == 0 {
			continue
		}

		providerName := ""
		if p, ok := providerInfoMap[providerID]; ok {
			providerName = p.ProviderName
		}

		instanceName := ""
		if inst, ok := instanceInfoMap[instanceID]; ok {
			instanceName = inst.InstanceName
		}

		addedModels = append(addedModels, AddedModelItem{
			ModelType:    modelTypes,
			Name:         modelName,
			ProviderID:   providerID,
			ProviderName: providerName,
			InstanceID:   instanceID,
			InstanceName: instanceName,
		})
	}

	return addedModels, nil
}
