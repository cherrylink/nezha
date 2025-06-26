package geoip

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// APIResponse 表示IP-API响应结构
type APIResponse struct {
	Status      string  `json:"status"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Zip         string  `json:"zip"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	ISP         string  `json:"isp"`
	Org         string  `json:"org"`
	AS          string  `json:"as"`
	Query       string  `json:"query"`
}

// 缓存条目
type cacheEntry struct {
	countryCode string
	asn         string
	timestamp   time.Time
}

// HTTP客户端配置
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// API基础URL
const apiBaseURL = "http://ip-api.com/json/"

// 缓存和频率限制
var (
	// IP查询结果缓存，避免重复查询同一IP
	ipCache = make(map[string]*cacheEntry)
	cacheMu sync.RWMutex

	// 请求频率限制，避免被API服务商拉黑
	lastRequestTime time.Time
	requestMu       sync.Mutex

	// 缓存过期时间：24小时
	cacheExpiry = 24 * time.Hour

	// 请求间隔限制：最少间隔2秒
	minRequestInterval = 2 * time.Second
)

// 检查缓存
func getCachedResult(ip string) (countryCode, asn string, found bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	entry, exists := ipCache[ip]
	if !exists {
		return "", "", false
	}

	// 检查缓存是否过期
	if time.Since(entry.timestamp) > cacheExpiry {
		return "", "", false
	}

	return entry.countryCode, entry.asn, true
}

// 存储到缓存
func setCachedResult(ip, countryCode, asn string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	ipCache[ip] = &cacheEntry{
		countryCode: countryCode,
		asn:         asn,
		timestamp:   time.Now(),
	}
}

// 频率限制检查
func checkRateLimit() {
	requestMu.Lock()
	defer requestMu.Unlock()

	elapsed := time.Since(lastRequestTime)
	if elapsed < minRequestInterval {
		sleepTime := minRequestInterval - elapsed
		time.Sleep(sleepTime)
	}
	lastRequestTime = time.Now()
}

// 查询IP地理位置信息
func queryIPAPI(ip net.IP) (*APIResponse, error) {
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address")
	}

	ipStr := ip.String()

	// 检查缓存
	if countryCode, asn, found := getCachedResult(ipStr); found {
		return &APIResponse{
			Status:      "success",
			CountryCode: countryCode,
			AS:          asn,
			Query:       ipStr,
		}, nil
	}

	// 应用频率限制
	checkRateLimit()

	url := apiBaseURL + ipStr

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status code: %d", resp.StatusCode)
	}

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API returned error status: %s", result.Status)
	}

	// 存储到缓存
	var asn string
	if result.AS != "" {
		parts := strings.SplitN(result.AS, " ", 2)
		if len(parts) > 1 {
			asn = parts[1]
		} else {
			asn = result.AS
		}
	} else if result.Org != "" {
		asn = result.Org
	}

	setCachedResult(ipStr, result.CountryCode, asn)

	return &result, nil
}

// Lookup 查询IP的国家代码
func Lookup(ip net.IP) (string, error) {
	result, err := queryIPAPI(ip)
	if err != nil {
		return "", err
	}

	if result.CountryCode != "" {
		return strings.ToLower(result.CountryCode), nil
	}

	return "", fmt.Errorf("country code not found for IP: %s", ip.String())
}

// LookupASN 查询IP的ASN组织名称
func LookupASN(ip net.IP) (string, error) {
	result, err := queryIPAPI(ip)
	if err != nil {
		return "", err
	}

	if result.AS != "" {
		// ASN字段格式通常是 "AS15169 Google LLC"
		// 我们只返回组织名称部分
		parts := strings.SplitN(result.AS, " ", 2)
		if len(parts) > 1 {
			return parts[1], nil
		}
		return result.AS, nil
	}

	// 如果AS字段为空，尝试使用Org字段
	if result.Org != "" {
		return result.Org, nil
	}

	return "", fmt.Errorf("ASN information not found for IP: %s", ip.String())
}

// LookupBoth 同时查询国家代码和ASN信息（优化：减少API调用次数）
func LookupBoth(ip net.IP) (countryCode, asn string, err error) {
	result, err := queryIPAPI(ip)
	if err != nil {
		return "", "", err
	}

	// 获取国家代码
	if result.CountryCode != "" {
		countryCode = strings.ToLower(result.CountryCode)
	}

	// 获取ASN信息
	if result.AS != "" {
		parts := strings.SplitN(result.AS, " ", 2)
		if len(parts) > 1 {
			asn = parts[1]
		} else {
			asn = result.AS
		}
	} else if result.Org != "" {
		asn = result.Org
	}

	return countryCode, asn, nil
}

// ClearCache 清理过期缓存（可选的维护功能）
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	now := time.Now()
	for ip, entry := range ipCache {
		if now.Sub(entry.timestamp) > cacheExpiry {
			delete(ipCache, ip)
		}
	}
}

// GetCacheStats 获取缓存统计信息（调试用）
func GetCacheStats() (total int, expired int) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	now := time.Now()
	total = len(ipCache)

	for _, entry := range ipCache {
		if now.Sub(entry.timestamp) > cacheExpiry {
			expired++
		}
	}

	return total, expired
}
