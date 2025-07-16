package main

import (
	json "github.com/json-iterator/go"
	"log"
	"os"
	"strings"
	"time"
)

// loadModels loads model definitions from models.json
func loadModels() ModelsData {
	var result ModelsData

	data, err := os.ReadFile("models.json")
	if err != nil {
		log.Printf("Error loading models.json: %v", err)
		anthropicModelMappings = make(map[string]string)
		return result
	}

	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		// Try old format (string array)
		var modelIDs []string
		if err := json.Unmarshal(data, &modelIDs); err != nil {
			log.Printf("Error parsing models.json: %v", err)
			anthropicModelMappings = make(map[string]string)
			return result
		}
		// Convert to new format
		config.Models = make(map[string]string)
		for _, modelID := range modelIDs {
			config.Models[modelID] = modelID
		}
		config.AnthropicModelMappings = make(map[string]string)
	}

	anthropicModelMappings = config.AnthropicModelMappings
	if anthropicModelMappings == nil {
		anthropicModelMappings = make(map[string]string)
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

	log.Printf("Loaded %d model mappings from models.json", len(anthropicModelMappings))
	return result
}

// loadClientAPIKeys loads client API keys from environment variables
func loadClientAPIKeys() {
	keysEnv := os.Getenv("CLIENT_API_KEYS")
	if keysEnv != "" {
		keys := strings.Split(keysEnv, ",")
		validClientKeys = make(map[string]bool)
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key != "" {
				validClientKeys[key] = true
			}
		}

		if len(validClientKeys) == 0 {
			log.Println("Warning: CLIENT_API_KEYS environment variable is empty")
		} else {
			log.Printf("Successfully loaded %d client API keys from environment", len(validClientKeys))
		}
	} else {
		log.Println("Error: CLIENT_API_KEYS environment variable not found")
		validClientKeys = make(map[string]bool)
	}
}

// loadJetbrainsAccounts loads JetBrains account information from environment variables
func loadJetbrainsAccounts() {
	licenseIDsEnv := os.Getenv("JETBRAINS_LICENSE_IDS")
	authorizationsEnv := os.Getenv("JETBRAINS_AUTHORIZATIONS")

	var licenseIDs, authorizations []string

	if licenseIDsEnv != "" {
		licenseIDs = strings.Split(licenseIDsEnv, ",")
		for i, id := range licenseIDs {
			licenseIDs[i] = strings.TrimSpace(id)
		}
	}

	if authorizationsEnv != "" {
		authorizations = strings.Split(authorizationsEnv, ",")
		for i, auth := range authorizations {
			authorizations[i] = strings.TrimSpace(auth)
		}
	}

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

