package main

import (
    "bufio"
    "bytes"
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "log"
    "mime/multipart"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/gorilla/websocket"
)

// Configuration for AmiVoice API
type Config struct {
    AppKey          string
    Interface       string // sync, async, websocket
    Engine          string
    AudioFile       string
    AudioFormat     string
    SampleRate      string
    ProfileID       string
    ProfileWords    string
    KeepFillerToken string
    LooseSymbol     string
    ResultType      string
    NoLog           bool
    Verbose         bool
    OutputFile      string
    PollingInterval int // for async interface
}

// Response structures
type SyncResponse struct {
    Results []Result `json:"results"`
    Text    string   `json:"text"`
    Code    string   `json:"code"`
    Message string   `json:"message"`
}

type Result struct {
    Tokens   []Token `json:"tokens"`
    Tags     []string `json:"tags"`
    Rulename string   `json:"rulename"`
    Text     string   `json:"text"`
}

type Token struct {
    Written      string  `json:"written"`
    Spoken       string  `json:"spoken"`
    StartTime    int     `json:"starttime"`
    EndTime      int     `json:"endtime"`
    Confidence   float64 `json:"confidence"`
}

type AsyncJobResponse struct {
    SessionID      string  `json:"sessionid"`
    Status         string  `json:"status"`
    AudioMD5       string  `json:"audio_md5"`
    AudioFileType  string  `json:"audio_file_type"`
    ServiceID      string  `json:"service_id"`
    ErrorMessage   string  `json:"error_message"`
    Results        []Result `json:"results"`
    Text           string   `json:"text"`
}

func main() {
    // .envファイルを読み込む
    loadEnvFile()
    
    config := parseFlags()
    
    if config.AudioFile == "" {
        log.Fatal("音声ファイルを指定してください (-file)")
    }
    
    if config.AppKey == "" {
        // 環境変数から取得を試みる
        config.AppKey = os.Getenv("AMIVOICE_APP_KEY")
        if config.AppKey == "" {
            log.Fatal("APP KEYを指定してください (-appkey または環境変数 AMIVOICE_APP_KEY)")
        }
    }
    
    var result string
    var err error
    
    switch config.Interface {
    case "sync":
        result, err = processSyncHTTP(config)
    case "async":
        result, err = processAsyncHTTP(config)
    case "websocket":
        result, err = processWebSocket(config)
    default:
        log.Fatalf("不正なインターフェース: %s (sync, async, websocket のいずれかを指定)", config.Interface)
    }
    
    if err != nil {
        log.Fatal("エラー:", err)
    }
    
    // 結果の出力
    if config.OutputFile != "" {
        err = os.WriteFile(config.OutputFile, []byte(result), 0644)
        if err != nil {
            log.Fatal("出力ファイルの書き込みエラー:", err)
        }
        fmt.Printf("結果を %s に保存しました\n", config.OutputFile)
    } else {
        fmt.Println(result)
    }
}

func parseFlags() *Config {
    config := &Config{}
    
    flag.StringVar(&config.AppKey, "appkey", "", "AmiVoice API APP KEY")
    flag.StringVar(&config.Interface, "interface", "sync", "インターフェース (sync/async/websocket)")
    flag.StringVar(&config.Engine, "engine", "-a-general", "音声認識エンジン")
    flag.StringVar(&config.AudioFile, "file", "", "音声ファイルパス (必須)")
    flag.StringVar(&config.AudioFormat, "format", "", "音声フォーマット (例: LSB16K, LSB8K)")
    flag.StringVar(&config.SampleRate, "samplerate", "", "サンプリングレート")
    flag.StringVar(&config.ProfileID, "profileid", "", "プロファイルID")
    flag.StringVar(&config.ProfileWords, "profilewords", "", "プロファイル単語")
    flag.StringVar(&config.KeepFillerToken, "keepfiller", "", "フィラー単語を保持 (1)")
    flag.StringVar(&config.LooseSymbol, "loosesymbol", "", "記号を緩く扱う (1)")
    flag.StringVar(&config.ResultType, "resulttype", "", "結果タイプ")
    flag.BoolVar(&config.NoLog, "nolog", false, "ログ保存なし")
    flag.BoolVar(&config.Verbose, "verbose", false, "詳細出力")
    flag.StringVar(&config.OutputFile, "output", "", "出力ファイルパス")
    flag.IntVar(&config.PollingInterval, "polling", 5, "非同期処理のポーリング間隔（秒）")
    
    flag.Parse()
    return config
}

func processSyncHTTP(config *Config) (string, error) {
    endpoint := getEndpoint("https://acp-api.amivoice.com/v1/recognize", config.NoLog)
    
    body, contentType, err := createMultipartForm(config, false)
    if err != nil {
        return "", err
    }
    
    if config.Verbose {
        log.Printf("エンドポイント: %s", endpoint)
        log.Printf("dパラメータ: %s", buildDParams(config))
    }
    
    respBody, err := doHTTPRequest("POST", endpoint, body, map[string]string{
        "Content-Type": contentType,
    }, 300*time.Second, config.Verbose)
    if err != nil {
        return "", err
    }
    
    var syncResp SyncResponse
    if err := json.Unmarshal(respBody, &syncResp); err != nil {
        return "", fmt.Errorf("JSONパースエラー: %v", err)
    }
    
    if syncResp.Code != "" && syncResp.Code != "0" {
        return "", fmt.Errorf("APIエラー: %s - %s", syncResp.Code, syncResp.Message)
    }
    
    return formatResponse(&syncResp, config), nil
}

func processAsyncHTTP(config *Config) (string, error) {
    endpoint := getEndpoint("https://acp-api-async.amivoice.com/v1/recognitions", config.NoLog)
    
    sessionID, err := startAsyncJob(config, endpoint)
    if err != nil {
        return "", err
    }
    
    if config.Verbose {
        log.Printf("セッションID: %s", sessionID)
    }
    
    for {
        status, result, err := checkAsyncJobStatus(config, endpoint, sessionID)
        if err != nil {
            return "", err
        }
        
        if config.Verbose {
            log.Printf("ステータス: %s", status)
        }
        
        if status == "completed" {
            return result, nil
        } else if status == "error" {
            return "", fmt.Errorf("音声認識エラー: %s", result)
        }
        
        time.Sleep(time.Duration(config.PollingInterval) * time.Second)
    }
}

func startAsyncJob(config *Config, endpoint string) (string, error) {
    body, contentType, err := createMultipartForm(config, true)
    if err != nil {
        return "", err
    }
    
    respBody, err := doHTTPRequest("POST", endpoint, body, map[string]string{
        "Content-Type": contentType,
    }, 60*time.Second, false)
    if err != nil {
        return "", err
    }
    
    var jobResp AsyncJobResponse
    if err := json.Unmarshal(respBody, &jobResp); err != nil {
        return "", fmt.Errorf("JSONパースエラー: %v", err)
    }
    
    return jobResp.SessionID, nil
}

func checkAsyncJobStatus(config *Config, endpoint string, sessionID string) (string, string, error) {
    url := fmt.Sprintf("%s/%s", endpoint, sessionID)
    
    respBody, err := doHTTPRequest("GET", url, nil, map[string]string{
        "Authorization": "Bearer " + config.AppKey,
    }, 30*time.Second, false)
    if err != nil {
        return "", "", err
    }
    
    var jobResp AsyncJobResponse
    if err := json.Unmarshal(respBody, &jobResp); err != nil {
        return "", "", fmt.Errorf("JSONパースエラー: %v", err)
    }
    
    if jobResp.Status == "completed" {
        syncResp := SyncResponse{
            Results: jobResp.Results,
            Text:    jobResp.Text,
        }
        return jobResp.Status, formatResponse(&syncResp, config), nil
    } else if jobResp.Status == "error" {
        return jobResp.Status, jobResp.ErrorMessage, nil
    }
    
    return jobResp.Status, "", nil
}

func processWebSocket(config *Config) (string, error) {
    endpoint := getWebSocketEndpoint(config.NoLog)
    
    dialer := websocket.Dialer{}
    conn, _, err := dialer.Dial(endpoint, nil)
    if err != nil {
        return "", fmt.Errorf("WebSocket接続エラー: %v", err)
    }
    defer conn.Close()
    
    if err := authenticateWebSocket(conn, config.AppKey); err != nil {
        return "", err
    }
    
    if err := startWebSocketRecognition(conn, config); err != nil {
        return "", err
    }
    
    if err := sendAudioDataWebSocket(conn, config.AudioFile); err != nil {
        return "", err
    }
    
    if err := conn.WriteMessage(websocket.TextMessage, []byte("e ")); err != nil {
        return "", err
    }
    
    return receiveWebSocketResults(conn, config.Verbose)
}

func buildDParams(config *Config) string {
    params := []string{
        fmt.Sprintf("grammarFileNames=%s", config.Engine),
    }
    
    if config.ProfileID != "" {
        params = append(params, fmt.Sprintf("profileId=%s", config.ProfileID))
    }
    
    if config.ProfileWords != "" {
        params = append(params, fmt.Sprintf("profileWords=%s", config.ProfileWords))
    }
    
    if config.KeepFillerToken != "" {
        params = append(params, fmt.Sprintf("keepFillerToken=%s", config.KeepFillerToken))
    }
    
    if config.LooseSymbol != "" {
        params = append(params, fmt.Sprintf("looseSymbol=%s", config.LooseSymbol))
    }
    
    if config.ResultType != "" {
        params = append(params, fmt.Sprintf("resultType=%s", config.ResultType))
    }
    
    return strings.Join(params, " ")
}

// Helper functions for common operations
func getEndpoint(baseURL string, noLog bool) string {
    if noLog {
        return strings.Replace(baseURL, "/v1/", "/v1/nolog/", 1)
    }
    return baseURL
}

func getWebSocketEndpoint(noLog bool) string {
    if noLog {
        return "wss://acp-api.amivoice.com/v1/nolog/"
    }
    return "wss://acp-api.amivoice.com/v1/"
}

func openAudioFile(filename string) (*os.File, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, fmt.Errorf("音声ファイルを開けません: %v", err)
    }
    return file, nil
}

func createMultipartForm(config *Config, useFormFile bool) (io.Reader, string, error) {
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    
    writer.WriteField("u", config.AppKey)
    writer.WriteField("d", buildDParams(config))
    
    if config.AudioFormat != "" {
        writer.WriteField("c", config.AudioFormat)
    }
    
    file, err := openAudioFile(config.AudioFile)
    if err != nil {
        return nil, "", err
    }
    defer file.Close()
    
    var part io.Writer
    if useFormFile {
        part, err = writer.CreateFormFile("a", filepath.Base(config.AudioFile))
    } else {
        part, err = writer.CreateFormField("a")
    }
    if err != nil {
        return nil, "", err
    }
    
    if _, err = io.Copy(part, file); err != nil {
        return nil, "", err
    }
    
    if err = writer.Close(); err != nil {
        return nil, "", err
    }
    
    return body, writer.FormDataContentType(), nil
}

func doHTTPRequest(method, url string, body io.Reader, headers map[string]string, timeout time.Duration, verbose bool) ([]byte, error) {
    req, err := http.NewRequest(method, url, body)
    if err != nil {
        return nil, err
    }
    
    for key, value := range headers {
        req.Header.Set(key, value)
    }
    
    client := &http.Client{Timeout: timeout}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    
    if verbose {
        log.Printf("HTTPステータス: %d", resp.StatusCode)
        log.Printf("レスポンス: %s", string(respBody))
    }
    
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("HTTPエラー: %d - %s", resp.StatusCode, string(respBody))
    }
    
    return respBody, nil
}

func authenticateWebSocket(conn *websocket.Conn, appKey string) error {
    authCmd := fmt.Sprintf("s %s", appKey)
    if err := conn.WriteMessage(websocket.TextMessage, []byte(authCmd)); err != nil {
        return err
    }
    
    _, msg, err := conn.ReadMessage()
    if err != nil {
        return err
    }
    
    if !strings.HasPrefix(string(msg), "s ") {
        return fmt.Errorf("認証エラー: %s", string(msg))
    }
    
    return nil
}

func startWebSocketRecognition(conn *websocket.Conn, config *Config) error {
    dParams := buildDParams(config)
    startCmd := fmt.Sprintf("s %s %s", config.AudioFormat, dParams)
    return conn.WriteMessage(websocket.TextMessage, []byte(startCmd))
}

func sendAudioDataWebSocket(conn *websocket.Conn, audioFile string) error {
    file, err := openAudioFile(audioFile)
    if err != nil {
        return err
    }
    defer file.Close()
    
    buffer := make([]byte, 4096)
    for {
        n, err := file.Read(buffer)
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        
        if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte("p "), buffer[:n]...)); err != nil {
            return err
        }
    }
    
    return nil
}

func receiveWebSocketResults(conn *websocket.Conn, verbose bool) (string, error) {
    var results []string
    for {
        _, msg, err := conn.ReadMessage()
        if err != nil {
            return "", err
        }
        
        msgStr := string(msg)
        if verbose {
            log.Printf("受信: %s", msgStr)
        }
        
        if strings.HasPrefix(msgStr, "A ") || strings.HasPrefix(msgStr, "U ") {
            parts := strings.SplitN(msgStr, " ", 2)
            if len(parts) > 1 {
                var result map[string]interface{}
                if err := json.Unmarshal([]byte(parts[1]), &result); err == nil {
                    if text, ok := result["text"].(string); ok && text != "" {
                        results = append(results, text)
                    }
                }
            }
        } else if strings.HasPrefix(msgStr, "e ") {
            break
        } else if strings.HasPrefix(msgStr, "E ") {
            return "", fmt.Errorf("WebSocketエラー: %s", msgStr)
        }
    }
    
    return strings.Join(results, "\n"), nil
}

func loadEnvFile() {
    // 現在のディレクトリの親ディレクトリの.envファイルを読み込む
    envPath := "../.env"
    
    file, err := os.Open(envPath)
    if err != nil {
        // .envファイルが存在しない場合は無視
        return
    }
    defer file.Close()
    
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        
        // 空行やコメント行をスキップ
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        
        // KEY=VALUE形式をパース
        parts := strings.SplitN(line, "=", 2)
        if len(parts) == 2 {
            key := strings.TrimSpace(parts[0])
            value := strings.TrimSpace(parts[1])
            
            // 既に環境変数が設定されている場合は上書きしない
            if os.Getenv(key) == "" {
                os.Setenv(key, value)
            }
        }
    }
}

func formatResponse(resp *SyncResponse, config *Config) string {
    if config.Verbose {
        // 詳細出力の場合はJSON形式
        jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
        return string(jsonBytes)
    }
    
    // 通常はテキストのみ
    return resp.Text
}