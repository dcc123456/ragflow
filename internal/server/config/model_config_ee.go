//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package config

import "github.com/spf13/viper"

// DefaultModelsConfig default models configuration
type DefaultModelsConfig struct {
	ChatModel      ModelConfig `mapstructure:"chat_model"`
	EmbeddingModel ModelConfig `mapstructure:"embedding_model"`
	RerankModel    ModelConfig `mapstructure:"rerank_model"`
	ASRModel       ModelConfig `mapstructure:"asr_model"`
	VisionModel    ModelConfig `mapstructure:"vision_model"`
	OCRModel       ModelConfig `mapstructure:"ocr_model"`
	TTSModel       ModelConfig `mapstructure:"tts_model"`
}

// ModelConfig model configuration
type ModelConfig struct {
	Name    string `mapstructure:"name"`
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
	Factory string `mapstructure:"factory"`
}

func (c *Config) ParseDefaultModelsConfig(v *viper.Viper) error {
	return nil
}

func (c *Config) GetDefaultChatModel() ModelConfig {
	return c.defaultModels.ChatModel
}

func (c *Config) GetDefaultEmbeddingModel() ModelConfig {
	return c.defaultModels.EmbeddingModel
}

func (c *Config) GetDefaultRerankModel() ModelConfig {
	return c.defaultModels.RerankModel
}

func (c *Config) GetDefaultASRModel() ModelConfig {
	return c.defaultModels.ASRModel
}

func (c *Config) GetDefaultVisionModel() ModelConfig {
	return c.defaultModels.VisionModel
}

func (c *Config) GetDefaultOCRModel() ModelConfig {
	return c.defaultModels.OCRModel
}

func (c *Config) GetDefaultTTSModel() ModelConfig {
	return c.defaultModels.TTSModel
}
