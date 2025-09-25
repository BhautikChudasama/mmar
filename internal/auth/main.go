package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Authentication error constants
var (
	ErrAuthTokenRequired = errors.New("authentication token is required")
	ErrAuthTokenInvalid  = errors.New("invalid authentication token")
)

type ApiKeyConfig struct {
	Key   string `json:"key"`
	Limit int    `json:"limit"`
}

type ApiKeysConfig []ApiKeyConfig

type AuthManager struct {
	mu            sync.RWMutex
	apiKeys       map[string]int
	tunnelsPerKey map[string][]string
	configFile    string
}

func NewAuthManager(configFile string) (*AuthManager, error) {
	am := &AuthManager{
		apiKeys:       make(map[string]int),
		tunnelsPerKey: make(map[string][]string),
		configFile:    configFile,
	}

	if err := am.loadApiKeys(); err != nil {
		return nil, fmt.Errorf("failed to load API keys: %v", err)
	}

	return am, nil
}

func (am *AuthManager) loadApiKeys() error {
	if am.configFile == "" {
		return fmt.Errorf("API keys file path not provided")
	}

	data, err := os.ReadFile(am.configFile)
	if err != nil {
		return fmt.Errorf("failed to read API keys file: %v", err)
	}

	var config ApiKeysConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse API keys file: %v", err)
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	am.apiKeys = func() map[string]int {
		m := make(map[string]int)
		for _, entry := range config {
			m[entry.Key] = entry.Limit
		}

		return m
	}()

	// print all keys and quota in key | limit format
	fmt.Println("Loaded API keys and their limits:")
	fmt.Println("-------------------------------------")
	for key, limit := range am.apiKeys {
		fmt.Printf("%s | %d\n", key, limit)
	}
	fmt.Println("-------------------------------------")

	return nil
}

func (am *AuthManager) ReloadApiKeys() error {
	return am.loadApiKeys()
}

func (am *AuthManager) ValidateToken(token string) (bool, int, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if token == "" {
		return false, 0, ErrAuthTokenRequired
	}

	limit, exists := am.apiKeys[token]
	if !exists {
		return false, 0, ErrAuthTokenInvalid
	}

	return true, limit, nil
}

func (am *AuthManager) CheckTunnelLimit(token string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()

	tunnels := am.tunnelsPerKey[token]
	limit := am.apiKeys[token]

	return len(tunnels) >= limit
}

func (am *AuthManager) AddTunnel(token string, tunnelId string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.tunnelsPerKey[token] == nil {
		am.tunnelsPerKey[token] = []string{}
	}
	am.tunnelsPerKey[token] = append(am.tunnelsPerKey[token], tunnelId)
}

func (am *AuthManager) RemoveTunnel(token string, tunnelId string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	tunnels := am.tunnelsPerKey[token]
	if tunnels == nil {
		return
	}

	for i, id := range tunnels {
		if id == tunnelId {
			am.tunnelsPerKey[token] = append(tunnels[:i], tunnels[i+1:]...)
			break
		}
	}
}

func (am *AuthManager) GetTunnelCount(token string) int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	tunnels := am.tunnelsPerKey[token]
	if tunnels == nil {
		return 0
	}
	return len(tunnels)
}

func (am *AuthManager) GetTokenLimit(token string) int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	limit, exists := am.apiKeys[token]
	if !exists {
		return 0
	}
	return limit
}

func (am *AuthManager) GetAllTokens() map[string]int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	result := make(map[string]int)
	for token, limit := range am.apiKeys {
		result[token] = limit
	}
	return result
}
