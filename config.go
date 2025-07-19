package main

import (
	"github.com/bytedance/sonic"
	"log"
	"os"
	"time"
)

// loadModels loads model definitions from models.json
func loadModels() ModelsData {
	var result ModelsData

	data, err := os.ReadFile("models.json")
	if err != nil {
		log.Printf("Error loading models.json: %v", err)
		return result
	}

	var config ModelsConfig
	if err := sonic.Unmarshal(data, &config); err != nil {
		// Try old format (string array)
		var modelIDs []string
		if err := sonic.Unmarshal(data, &modelIDs); err != nil {
			log.Printf("Error parsing models.json: %v", err)
				return result
		}
		// Convert to new format
		config.Models = make(map[string]string)
		for _, modelID := range modelIDs {
			config.Models[modelID] = modelID
		}
	}

	now := time.Now().Unix()
	for modelKey := range config.Models {
		result.Data = append(result.Data, ModelInfo{
			ID:      modelKey,
			Object:  "model",
			Created: now,
			OwnedBy: "jetbrains-ai",
		})
	}

	log.Printf("Loaded %d models from models.json", len(config.Models))
	return result
}

// loadClientAPIKeys loads client API keys from environment variables
func loadClientAPIKeys() {
	keys := parseEnvList(os.Getenv("CLIENT_API_KEYS"))
	validClientKeys = make(map[string]bool)
	for _, key := range keys {
		validClientKeys[key] = true
	}

	if len(validClientKeys) == 0 {
		log.Println("Warning: CLIENT_API_KEYS environment variable is empty")
	} else {
		log.Printf("Successfully loaded %d client API keys from environment", len(validClientKeys))
	}
}

// loadJetbrainsAccounts loads JetBrains account information from environment variables
func loadJetbrainsAccounts() {
	licenseIDsEnv := os.Getenv("JETBRAINS_LICENSE_IDS")
	authorizationsEnv := os.Getenv("JETBRAINS_AUTHORIZATIONS")

	licenseIDs := parseEnvList(licenseIDsEnv)
	authorizations := parseEnvList(authorizationsEnv)

	maxLen := len(licenseIDs)
	if len(authorizations) > maxLen {
		maxLen = len(authorizations)
	}

	for len(licenseIDs) < maxLen {
		licenseIDs = append(licenseIDs, "")
	}
	for len(authorizations) < maxLen {
		authorizations = append(authorizations, "")
	}

	jetbrainsAccounts = []JetbrainsAccount{}
	for i := 0; i < maxLen; i++ {
		if licenseIDs[i] != "" && authorizations[i] != "" {
			account := JetbrainsAccount{
				LicenseID:      licenseIDs[i],
				Authorization:  authorizations[i],
				JWT:            "",
				LastUpdated:    0,
				HasQuota:       true,
				LastQuotaCheck: 0,
			}
			jetbrainsAccounts = append(jetbrainsAccounts, account)
		}
	}

	if len(jetbrainsAccounts) == 0 {
		log.Println("Warning: No valid JetBrains accounts found in environment variables")
	} else {
		log.Printf("Successfully loaded %d JetBrains AI accounts from environment", len(jetbrainsAccounts))
	}
}

func getInternalModelName(modelID string) string {
	if internalModel, exists := modelsConfig.Models[modelID]; exists {
		return internalModel
	}
	return modelID
}

func getModelItem(modelID string) *ModelInfo {
	for _, model := range modelsData.Data {
		if model.ID == modelID {
			return &model
		}
	}
	return nil
}

