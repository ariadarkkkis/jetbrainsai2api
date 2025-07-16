package main

import (
	"bytes"
	json "github.com/json-iterator/go"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// setJetbrainsHeaders sets the required headers for JetBrains API requests
func setJetbrainsHeaders(req *http.Request, jwt string) {
	req.Header.Set("User-Agent", "ktor-client")
	req.Header.Set("Accept-Charset", "UTF-8")
	req.Header.Set("grazie-agent", `{"name":"aia:pycharm","version":"251.26094.80.13:251.26094.141"}`)
	if jwt != "" {
		req.Header.Set("grazie-authenticate-jwt", jwt)
	}
}

// handleJWTExpiredAndRetry handles JWT expiration and retries the request
func handleJWTExpiredAndRetry(req *http.Request, account *JetbrainsAccount) (*http.Response, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 401 && account.LicenseID != "" {
		resp.Body.Close()
		log.Printf("JWT for %s expired, refreshing...", getAccountIdentifier(account))
		if err := refreshJetbrainsJWT(account); err != nil {
			return nil, err
		}

		req.Header.Set("grazie-authenticate-jwt", account.JWT)
		return httpClient.Do(req)
	}

	return resp, nil
}

// ensureValidJWT ensures that the account has a valid JWT
func ensureValidJWT(account *JetbrainsAccount) error {
	if account.JWT == "" && account.LicenseID != "" {
		return refreshJetbrainsJWT(account)
	}
	return nil
}

// checkQuota checks the quota for a given JetBrains account
func checkQuota(account *JetbrainsAccount) error {
	if err := ensureValidJWT(account); err != nil {
		return err
	}

	if account.JWT == "" {
		account.HasQuota = false
		return nil
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/quota/get", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Length", "0")
	setJetbrainsHeaders(req, account.JWT)

	resp, err := handleJWTExpiredAndRetry(req, account)
	if err != nil {
		account.HasQuota = false
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		account.HasQuota = false
		return fmt.Errorf("quota check failed with status %d", resp.StatusCode)
	}

	var quotaData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		account.HasQuota = false
		return err
	}

	processQuotaData(quotaData, account)

	return nil
}

// refreshJetbrainsJWT refreshes the JWT for a given JetBrains account
func refreshJetbrainsJWT(account *JetbrainsAccount) error {
	log.Printf("Refreshing JWT for licenseId %s...", account.LicenseID)

	payload := map[string]string{"licenseId": account.LicenseID}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/auth/jetbrains-jwt/provide-access/license/v2", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", "Bearer "+account.Authorization)
	setJetbrainsHeaders(req, "")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("JWT refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}

	state, _ := data["state"].(string)
	token, _ := data["token"].(string)

	if state == "PAID" && token != "" {
		account.JWT = token
		account.LastUpdated = float64(time.Now().Unix())
		log.Printf("Successfully refreshed JWT for licenseId %s", account.LicenseID)
		return nil
	}

	return fmt.Errorf("JWT refresh failed: invalid response state %s", state)
}

// getNextJetbrainsAccount gets the next available JetBrains account
func getNextJetbrainsAccount() (*JetbrainsAccount, error) {
	if len(jetbrainsAccounts) == 0 {
		return nil, fmt.Errorf("service unavailable: no JetBrains accounts configured")
	}

	accountRotationLock.Lock()
	defer accountRotationLock.Unlock()

	for i := 0; i < len(jetbrainsAccounts); i++ {
		account := &jetbrainsAccounts[currentAccountIndex]
		currentAccountIndex = (currentAccountIndex + 1) % len(jetbrainsAccounts)

		now := time.Now().Unix()
		isQuotaStale := float64(now)-account.LastQuotaCheck > QuotaCacheTime.Seconds()

		if account.HasQuota && isQuotaStale {
			checkQuota(account)
		}

		if account.HasQuota {
			if account.LicenseID != "" {
				isJWTStale := float64(now)-account.LastUpdated > JWTRefreshTime.Seconds()
				if account.JWT == "" || isJWTStale {
					if err := refreshJetbrainsJWT(account); err != nil {
						log.Printf("Failed to refresh JWT: %v", err)
						continue
					}
					if !account.HasQuota {
						checkQuota(account)
						if !account.HasQuota {
							continue
						}
					}
				}
			}

			if account.JWT != "" {
				return account, nil
			}
		}
	}

	return nil, fmt.Errorf("all JetBrains accounts are over quota or invalid")
}

// processQuotaData processes quota data and updates account status
func processQuotaData(quotaData map[string]any, account *JetbrainsAccount) (dailyUsed, dailyTotal float64) {
	dailyUsed, dailyTotal = extractQuotaUsage(quotaData)

	if dailyTotal == 0 {
		dailyTotal = 1 // Avoid division by zero
	}

	account.HasQuota = dailyUsed < dailyTotal
	if !account.HasQuota {
		log.Printf("Account %s has no quota", getAccountIdentifier(account))
	}

	if expiryTime := parseExpiryTime(quotaData); !expiryTime.IsZero() {
		account.ExpiryTime = expiryTime
	}

	account.LastQuotaCheck = float64(time.Now().Unix())
	return dailyUsed, dailyTotal
}

func parseExpiryTime(quotaData map[string]any) time.Time {
	if untilStr, ok := quotaData["until"].(string); ok {
		parsedTime, err := time.Parse(time.RFC3339Nano, untilStr)
		if err != nil {
			parsedTime, err = time.Parse(time.RFC3339, untilStr)
		}
		if err == nil {
			return parsedTime
		}
		log.Printf("Warning: could not parse 'until' date '%s': %v", untilStr, err)
	}
	return time.Time{}
}

func extractQuotaUsage(quotaData map[string]any) (float64, float64) {
	var dailyUsed, dailyTotal float64

	if current, ok := quotaData["current"].(map[string]any); ok {
		if currentUsage, ok := current["current"].(map[string]any); ok {
			if amountStr, ok := currentUsage["amount"].(string); ok {
				if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
					dailyUsed = parsed
				}
			}
		}

		if maximum, ok := current["maximum"].(map[string]any); ok {
			if amountStr, ok := maximum["amount"].(string); ok {
				if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
					dailyTotal = parsed
				}
			}
		}
	}
	return dailyUsed, dailyTotal
}

func getQuotaData(account *JetbrainsAccount) (map[string]any, error) {
	if err := ensureValidJWT(account); err != nil {
		return nil, fmt.Errorf("failed to refresh JWT: %w", err)
	}

	if account.JWT == "" {
		return nil, fmt.Errorf("account has no JWT")
	}

	req, err := http.NewRequest("POST", "https://api.jetbrains.ai/user/v5/quota/get", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Length", "0")
	setJetbrainsHeaders(req, account.JWT)

	resp, err := handleJWTExpiredAndRetry(req, account)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("quota check failed with status %d: %s", resp.StatusCode, string(body))
	}

	var quotaData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		return nil, err
	}

	if gin.Mode() == gin.DebugMode {
		quotaJSON, _ := json.MarshalIndent(quotaData, "", "  ")
		log.Printf("JetBrains Quota API Response: %s", string(quotaJSON))
	}

	processQuotaData(quotaData, account)

	quotaData["HasQuota"] = account.HasQuota
	quotaData["expiryTime"] = account.ExpiryTime

	return quotaData, nil
}


