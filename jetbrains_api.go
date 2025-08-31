package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bytedance/sonic"

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
		Info("JWT for %s expired, refreshing...", getTokenDisplayName(account))

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
	quotaData, err := getQuotaData(account)
	if err != nil {
		account.HasQuota = false
		return err
	}

	processQuotaData(quotaData, account)
	return nil
}

// refreshJetbrainsJWT refreshes the JWT for a given JetBrains account
func refreshJetbrainsJWT(account *JetbrainsAccount) error {
	Info("Refreshing JWT for licenseId %s...", account.LicenseID)

	payload := map[string]string{"licenseId": account.LicenseID}
	req, err := createJetbrainsRequest("POST", "https://api.jetbrains.ai/auth/jetbrains-jwt/provide-access/license/v2", payload, account.Authorization)
	if err != nil {
		return err
	}
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
			Warn("could not parse JWT: %v", err)
		} else if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if exp, ok := claims["exp"].(float64); ok {
				account.ExpiryTime = time.Unix(int64(exp), 0)
			}
		}

		Info("Successfully refreshed JWT for licenseId %s, expires at %s", account.LicenseID, account.ExpiryTime.Format(time.RFC3339))
		return nil
	}

	return fmt.Errorf("JWT refresh failed: invalid response state %s", state)
}

// getNextJetbrainsAccount gets the next available JetBrains account from the pool
func getNextJetbrainsAccount() (*JetbrainsAccount, error) {
	if len(jetbrainsAccounts) == 0 {
		return nil, fmt.Errorf("service unavailable: no JetBrains accounts configured")
	}

	accountWaitStart := time.Now()
	select {
	case account := <-accountPool:
		// 记录账户池等待时间
		waitDuration := time.Since(accountWaitStart)
		if waitDuration > 100*time.Millisecond { // 只记录超过100ms的等待
			RecordAccountPoolWait(waitDuration)
		}

		// Defer re-queueing the account
		defer func() {
			accountPool <- account
		}()

		// 检查JWT是否需要刷新
		if account.LicenseID != "" {
			if account.JWT == "" || time.Now().After(account.ExpiryTime.Add(-JWTRefreshTime)) {
				if err := refreshJetbrainsJWT(account); err != nil {
					Error("Failed to refresh JWT for %s: %v", getTokenDisplayName(account), err)
					RecordAccountPoolError()
					return nil, err // Return error to retry with another account
				}
			}
		}

		// 检查配额
		if err := checkQuota(account); err != nil {
			Error("Failed to check quota for %s: %v", getTokenDisplayName(account), err)
			RecordAccountPoolError()
			return nil, err // Return error to retry
		}

		if account.HasQuota {
			return account, nil
		}

		return nil, fmt.Errorf("account %s is over quota", getTokenDisplayName(account))

	case <-time.After(60 * time.Second): // 增加到60秒，给账户更多时间释放
		RecordAccountPoolError()
		return nil, fmt.Errorf("timed out waiting for an available JetBrains account")
	}
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
		Warn("Account %s has no quota", getTokenDisplayName(account))
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

	// 检查缓存
	quotaCacheMutex.RLock()
	cacheKey := account.JWT
	if cachedInfo, found := accountQuotaCache[cacheKey]; found && time.Since(cachedInfo.LastAccess) < QuotaCacheTime {
		quotaCacheMutex.RUnlock()
		return cachedInfo.QuotaData, nil
	}
	quotaCacheMutex.RUnlock()

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
		// 如果是401，则JWT可能已失效，从缓存中删除
		if resp.StatusCode == 401 {
			quotaCacheMutex.Lock()
			delete(accountQuotaCache, cacheKey)
			quotaCacheMutex.Unlock()
		}
		return nil, fmt.Errorf("quota check failed with status %d: %s", resp.StatusCode, string(body))
	}

	var quotaData JetbrainsQuotaResponse
	if err := sonic.ConfigDefault.NewDecoder(resp.Body).Decode(&quotaData); err != nil {
		return nil, err
	}

	// 更新缓存
	quotaCacheMutex.Lock()
	accountQuotaCache[cacheKey] = &CachedQuotaInfo{
		QuotaData:  &quotaData,
		LastAccess: time.Now(),
	}
	quotaCacheMutex.Unlock()

	if IsDebug() {
		quotaJSON, _ := sonic.MarshalIndent(quotaData, "", "  ")
		Debug("JetBrains Quota API Response: %s", string(quotaJSON))
	}

	processQuotaData(&quotaData, account)

	return &quotaData, nil
}
