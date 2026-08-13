package main

import (
	"bytes"
	crand "crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Simple .env file loader
func loadEnv() {
	envPath := ".env"
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			if key != "" && os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

// SearchRequest represents incoming search query
type SearchRequest struct {
	GSTIN      string `json:"gstin"`
	Captcha    string `json:"captcha"`
	CookiesB64 string `json:"cookies_b64"`
	AutoSolve  bool   `json:"auto_solve"`
}

// Session and Auth Management
type Session struct {
	Username  string
	CreatedAt time.Time
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var (
	sessionsMutex sync.RWMutex
	sessions      = make(map[string]Session)
)

const (
	sessionCookieName = "gst_session"
	sessionDuration   = 24 * time.Hour
)

func generateSessionToken() string {
	b := make([]byte, 32)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func getAdminCredentials() (string, string) {
	username := os.Getenv("ADMIN_USERNAME")
	if username == "" {
		username = "admin"
	}
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		password = "{bcU$-kx%ndbO~qy"
	}
	return username, password
}

func isValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	sessionsMutex.RLock()
	sess, exists := sessions[cookie.Value]
	sessionsMutex.RUnlock()
	if !exists {
		return false
	}
	if time.Since(sess.CreatedAt) > sessionDuration {
		sessionsMutex.Lock()
		delete(sessions, cookie.Value)
		sessionsMutex.Unlock()
		return false
	}
	return true
}

var (
	appApiKey     string
	appApiKeyOnce sync.Once
)

func getAppAPIKey() string {
	appApiKeyOnce.Do(func() {
		key := strings.TrimSpace(os.Getenv("APP_API_KEY"))
		if key == "" || key == "your_app_api_key_here" {
			key = "gst_sec_" + generateSessionToken()
			os.Setenv("APP_API_KEY", key)
			log.Printf("[INFO] APP_API_KEY was not set. Generated random key: %s", key)
		}
		appApiKey = key
	})
	return appApiKey
}

func isValidApiKey(r *http.Request) bool {
	configuredKey := getAppAPIKey()
	if configuredKey == "" {
		return false
	}

	// 1. Check X-API-Key header
	apiKeyHeader := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKeyHeader != "" && apiKeyHeader == configuredKey {
		return true
	}

	// 2. Check Authorization Bearer header
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			if token == configuredKey {
				return true
			}
		} else if authHeader == configuredKey {
			return true
		}
	}

	// 3. Check query parameter api_key
	queryKey := strings.TrimSpace(r.URL.Query().Get("api_key"))
	if queryKey != "" && queryKey == configuredKey {
		return true
	}

	return false
}

func isValidRequest(r *http.Request) bool {
	return isValidSession(r) || isValidApiKey(r)
}

func requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isValidRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "UNAUTHORIZED",
				"detail":  "Authentication required. Provide a valid session cookie or X-API-Key header.",
			})
			return
		}
		handler(w, r)
	}
}

// Krutrim API Structs
type KrutrimMessageContent struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL map[string]string `json:"image_url,omitempty"`
}

type KrutrimMessage struct {
	Role    string                  `json:"role"`
	Content []KrutrimMessageContent `json:"content"`
}

type KrutrimPayload struct {
	Model    string           `json:"model"`
	Messages []KrutrimMessage `json:"messages"`
}

type KrutrimResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type CookieStruct struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Helpers

func createClient() (*http.Client, *cookiejar.Jar) {
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		},
		DisableKeepAlives: false,
		MaxIdleConns:      10,
		IdleConnTimeout:   30 * time.Second,
	}
	client := &http.Client{
		Jar:       jar,
		Timeout:   15 * time.Second,
		Transport: tr,
	}
	return client, jar
}

func addHeaders(req *http.Request, acceptHeader string) {
	if acceptHeader == "" {
		acceptHeader = "application/json, text/plain, */*"
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,hi;q=0.8")
	req.Header.Set("Origin", "https://services.gst.gov.in")
	req.Header.Set("Referer", "https://services.gst.gov.in/services/searchtp")
	req.Header.Set("Sec-Ch-Ua", `"Not A(Brand";v="99", "Google Chrome";v="121", "Chromium";v="121"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
}

func ensureSession(client *http.Client) {
	u, _ := url.Parse("https://services.gst.gov.in")
	if len(client.Jar.Cookies(u)) > 0 {
		return
	}
	req, err := http.NewRequest("GET", "https://services.gst.gov.in/services/searchtp", nil)
	if err != nil {
		return
	}
	addHeaders(req, "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	resp, err := client.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func serializeCookies(jar *cookiejar.Jar) string {
	u, _ := url.Parse("https://services.gst.gov.in")
	cookies := jar.Cookies(u)
	var list []CookieStruct
	for _, c := range cookies {
		list = append(list, CookieStruct{Name: c.Name, Value: c.Value})
	}
	data, _ := json.Marshal(list)
	return base64.StdEncoding.EncodeToString(data)
}

func deserializeCookies(jar *cookiejar.Jar, cookiesB64 string) {
	u, _ := url.Parse("https://services.gst.gov.in")
	data, err := base64.StdEncoding.DecodeString(cookiesB64)
	if err != nil {
		return
	}
	var list []CookieStruct
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	var httpCookies []*http.Cookie
	for _, c := range list {
		httpCookies = append(httpCookies, &http.Cookie{
			Name:  c.Name,
			Value: c.Value,
		})
	}
	jar.SetCookies(u, httpCookies)
}

func fetchCaptcha(client *http.Client) ([]byte, error) {
	ensureSession(client)

	rnd := rand.Float64()
	urlStr := fmt.Sprintf("https://services.gst.gov.in/services/captcha?rnd=%f", rnd)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	addHeaders(req, "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CAPTCHA fetch returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func solveCaptchaWithKrutrim(rawBytes []byte, apiKey string) (string, error) {
	if apiKey == "" || apiKey == "your_krutrim_api_key_here" {
		return "", fmt.Errorf("invalid API key")
	}

	b64Img := base64.StdEncoding.EncodeToString(rawBytes)
	imgDataURL := "data:image/jpeg;base64," + b64Img

	payload := KrutrimPayload{
		Model: "gemma-4-31b-it",
		Messages: []KrutrimMessage{
			{
				Role: "user",
				Content: []KrutrimMessageContent{
					{
						Type: "image_url",
						ImageURL: map[string]string{
							"url": imgDataURL,
						},
					},
					{
						Type: "text",
						Text: "Decode this Captcha, there will be always number from 0-9 and it will have 6 digits, in the response just give the decoded number",
					},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://cloud.olakrutrim.com/v1/chat/completions", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	kClient := &http.Client{Timeout: 25 * time.Second}
	resp, err := kClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	resBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Krutrim HTTP %d: %s", resp.StatusCode, string(resBytes))
	}

	var kResp KrutrimResponse
	if err := json.Unmarshal(resBytes, &kResp); err != nil {
		return "", err
	}

	if len(kResp.Choices) == 0 {
		return "", fmt.Errorf("empty choices from Krutrim")
	}

	content := kResp.Choices[0].Message.Content
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(content, "")

	if len(digits) == 6 {
		return digits, nil
	}

	return "", fmt.Errorf("raw output '%s' did not yield 6 digits (got '%s')", strings.TrimSpace(content), digits)
}

func lookupGSTIN(client *http.Client, gstin, captcha string) (map[string]interface{}, error) {
	urlStr := "https://services.gst.gov.in/services/api/search/taxpayerDetails"
	payload := map[string]string{
		"gstin":   strings.ToUpper(strings.TrimSpace(gstin)),
		"captcha": strings.TrimSpace(captcha),
	}

	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}

	addHeaders(req, "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	resBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resBytes, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// Handlers

func handleServeUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	indexPath := filepath.Join("templates", "index.html")
	http.ServeFile(w, r, indexPath)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Invalid request payload"})
		return
	}

	adminUser, adminPass := getAdminCredentials()
	if req.Username != adminUser || req.Password != adminPass {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Invalid username or password"})
		return
	}

	token := generateSessionToken()
	sessionsMutex.Lock()
	sessions[token] = Session{
		Username:  adminUser,
		CreatedAt: time.Now(),
	}
	sessionsMutex.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionDuration),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"username": adminUser,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		sessionsMutex.Lock()
		delete(sessions, cookie.Value)
		sessionsMutex.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	if !isValidSession(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": false})
		return
	}

	adminUser, _ := getAdminCredentials()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		"username":      adminUser,
	})
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	krutrimKey := strings.TrimSpace(os.Getenv("KRUTRIM_API_KEY"))
	hasKrutrim := krutrimKey != "" && krutrimKey != "your_krutrim_api_key_here"

	appKey := getAppAPIKey()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated":     true,
		"krutrim_available": hasKrutrim,
		"app_api_key":       appKey,
	})
}

func handleGetCaptcha(w http.ResponseWriter, r *http.Request) {
	client, jar := createClient()
	rawBytes, err := fetchCaptcha(client)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"detail":  "Failed to fetch CAPTCHA from GST portal: " + err.Error(),
		})
		return
	}

	b64Img := base64.StdEncoding.EncodeToString(rawBytes)
	cookiesB64 := serializeCookies(jar)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"captcha_image": "data:image/jpeg;base64," + b64Img,
		"cookies_b64":   cookiesB64,
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	gstin := strings.ToUpper(strings.TrimSpace(req.GSTIN))
	if len(gstin) != 15 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"detail": "GSTIN must be exactly 15 characters long."})
		return
	}

	apiKey := strings.TrimSpace(os.Getenv("KRUTRIM_API_KEY"))
	hasKey := apiKey != "" && apiKey != "your_krutrim_api_key_here"

	// Manual solving mode
	if req.Captcha != "" && req.CookiesB64 != "" && !req.AutoSolve {
		client, jar := createClient()
		deserializeCookies(jar, req.CookiesB64)

		data, err := lookupGSTIN(client, gstin, req.Captcha)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"detail": "Lookup failed: " + err.Error()})
			return
		}

		if errCode, ok := data["errorCode"].(string); ok && errCode == "SWEB_9000" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "INVALID_CAPTCHA",
				"message": "Wrong CAPTCHA — rejected by GST server.",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    data,
			"method":  "manual",
		})
		return
	}

	// Auto-solve mode
	if req.AutoSolve {
		if !hasKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "NO_API_KEY",
				"message": "Krutrim API key is missing. Please set KRUTRIM_API_KEY in .env or solve manually.",
			})
			return
		}

		client, _ := createClient()
		maxAttempts := 5
		lastErr := "Could not decode CAPTCHA"

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				time.Sleep(400 * time.Millisecond)
			}
			rawBytes, err := fetchCaptcha(client)
			if err != nil {
				lastErr = fmt.Sprintf("Attempt %d fetch error: %v", attempt, err)
				continue
			}

			captchaText, err := solveCaptchaWithKrutrim(rawBytes, apiKey)
			if err != nil {
				lastErr = fmt.Sprintf("Attempt %d AI solver error: %v", attempt, err)
				continue
			}

			data, err := lookupGSTIN(client, gstin, captchaText)
			if err != nil {
				lastErr = fmt.Sprintf("Attempt %d lookup error: %v", attempt, err)
				continue
			}

			if errCode, ok := data["errorCode"].(string); ok && errCode == "SWEB_9000" {
				lastErr = fmt.Sprintf("Attempt %d rejected by GST server (%s)", attempt, captchaText)
				continue
			}

			// Success
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":      true,
				"data":         data,
				"method":       "auto_ai",
				"captcha_used": captchaText,
				"attempts":     attempt,
			})
			return
		}

		// Auto solve failed after max attempts, fallback
		freshBytes, err := fetchCaptcha(client)
		if err == nil {
			b64Img := base64.StdEncoding.EncodeToString(freshBytes)
			cookiesB64 := serializeCookies(client.Jar.(*cookiejar.Jar))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":       false,
				"error":         "AUTO_SOLVE_FAILED",
				"message":       fmt.Sprintf("Auto CAPTCHA solving failed after %d attempts: %s", maxAttempts, lastErr),
				"captcha_image": "data:image/jpeg;base64," + b64Img,
				"cookies_b64":   cookiesB64,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"detail": "Auto solve failed: " + lastErr,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{"detail": "Invalid request parameters."})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	loadEnv()

	port := os.Getenv("PORT")
	if port == "" {
		port = "4192"
	}

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/api/health", handleHealth)
	http.HandleFunc("/favicon.ico", handleFavicon)

	http.HandleFunc("/", handleServeUI)
	http.HandleFunc("/api/login", handleLogin)
	http.HandleFunc("/api/logout", handleLogout)
	http.HandleFunc("/api/me", handleMe)

	http.HandleFunc("/api/config", requireAuth(handleGetConfig))
	http.HandleFunc("/api/captcha", requireAuth(handleGetCaptcha))
	http.HandleFunc("/api/search", requireAuth(handleSearch))

	log.Printf("Starting GST Lookup Go Server on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
