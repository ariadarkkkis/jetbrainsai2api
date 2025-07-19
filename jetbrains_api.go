package main

import (
	"bytes"
	"github.com/bytedance/sonic"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var jwtRefreshMutex sync.Mutex

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
		log.Printf("JWT for %s expired, refreshing...", getTokenDisplayName(account))
		
		jwtRefreshMutex.Lock()
		// Check if another goroutine already refreshed the JWT
		if req.Header.Get("grazie-authenticate-jwt") == account.JWT {
			if err := refreshJetbrainsJWT(account); err != nil {
				jwtRefreshMutex.Unlock()
				return nil, err
			}
		}
		jwtRefreshMutex.Unlock()

		req.Header.Set("grazie-authenticate-jwt", account.JWT)
		return httpClient.Do(req)
	}

	return resp, nil
}

// ensureValidJWT ensures that the account has a valid JWT
func ensureValidJWT(account *JetbrainsAccount) error {
	if account.JWT == "" && account.LicenseID != "" {
		jwtRefreshMutex.Lock()
		defer jwtRefreshMutex.Unlock()
		
		// Double-check after acquiring lock
		if account.JWT == "" {
			return refreshJetbrainsJWT(account)
		}
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

	var quotaData JetbrainsQuotaResponse
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		account.HasQuota = false
		return err
	}

	processQuotaData(&quotaData, account)

	return nil
}

// refreshJetbrainsJWT refreshes the JWT for a given JetBrains account
func refreshJetbrainsJWT(account *JetbrainsAccount) error {
	log.Printf("Refreshing JWT for licenseId %s...", account.LicenseID)

	payload := map[string]string{"licenseId": account.LicenseID}
	payloadBytes, err := sonic.Marshal(payload)
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
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}

	state, _ := data["state"].(string)
	tokenStr, _ := data["token"].(string)

	if state == "PAID" && tokenStr != "" {
		account.JWT = tokenStr
		account.LastUpdated = float64(time.Now().Unix())

		// Parse the JWT to get the expiration time
		token, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
		if err != nil {
			log.Printf("Warning: could not parse JWT: %v", err)
		} else if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if exp, ok := claims["exp"].(float64); ok {
				account.ExpiryTime = time.Unix(int64(exp), 0)
			}
		}

		log.Printf("Successfully refreshed JWT for licenseId %s, expires at %s", account.LicenseID, account.ExpiryTime.Format(time.RFC3339))
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
func processQuotaData(quotaData *JetbrainsQuotaResponse, account *JetbrainsAccount) {
	dailyUsed, _ := strconv.ParseFloat(quotaData.Current.Current.Amount, 64)
	dailyTotal, _ := strconv.ParseFloat(quotaData.Current.Maximum.Amount, 64)

	if dailyTotal == 0 {
		dailyTotal = 1 // Avoid division by zero
	}

	account.HasQuota = dailyUsed < dailyTotal
	if !account.HasQuota {
		log.Printf("Account %s has no quota", getTokenDisplayName(account))
	}

	account.LastQuotaCheck = float64(time.Now().Unix())
}

func getQuotaData(account *JetbrainsAccount) (*JetbrainsQuotaResponse, error) {
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

	var quotaData JetbrainsQuotaResponse
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		return nil, err
	}

	if gin.Mode() == gin.DebugMode {
		quotaJSON, _ := sonic.MarshalIndent(quotaData, "", "  ")
		log.Printf("JetBrains Quota API Response: %s", string(quotaJSON))
	}

	processQuotaData(&quotaData, account)

	return &quotaData, nil
}


