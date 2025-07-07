package main

import (
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
    // エンドポイントの決定
    endpoint := "https://acp-api.amivoice.com/v1/recognize"
    if config.NoLog {
        endpoint = "https://acp-api.amivoice.com/v1/nolog/recognize"
    }
    
    // マルチパートフォームの作成
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    
    // APP KEY
    writer.WriteField("u", config.AppKey)
    
    // dパラメータの構築
    dParams := buildDParams(config)
    writer.WriteField("d", dParams)
    
    // 音声フォーマット
    if config.AudioFormat != "" {
        writer.WriteField("c", config.AudioFormat)
    }
    
    // 音声ファイル
    file, err := os.Open(config.AudioFile)
    if err != nil {
        return "", fmt.Errorf("音声ファイルを開けません: %v", err)
    }
    defer file.Close()
    
    part, err := writer.CreateFormField("a")
    if err != nil {
        return "", err
    }
    
    _, err = io.Copy(part, file)
    if err != nil {
        return "", err
    }
    
    err = writer.Close()
    if err != nil {
        return "", err
    }
    
    // HTTPリクエストの作成と送信
    if config.Verbose {
        log.Printf("エンドポイント: %s", endpoint)
        log.Printf("dパラメータ: %s", dParams)
    }
    
    req, err := http.NewRequest("POST", endpoint, body)
    if err != nil {
        return "", err
    }
    req.Header.Set("Content-Type", writer.FormDataContentType())
    
    client := &http.Client{Timeout: 300 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    // レスポンスの読み取り
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }
    
    if config.Verbose {
        log.Printf("HTTPステータス: %d", resp.StatusCode)
        log.Printf("レスポンス: %s", string(respBody))
    }
    
    // JSONのパース
    var syncResp SyncResponse
    err = json.Unmarshal(respBody, &syncResp)
    if err != nil {
        return "", fmt.Errorf("JSONパースエラー: %v", err)
    }
    
    if syncResp.Code != "" && syncResp.Code != "0" {
        return "", fmt.Errorf("APIエラー: %s - %s", syncResp.Code, syncResp.Message)
    }
    
    return formatResponse(&syncResp, config), nil
}

func processAsyncHTTP(config *Config) (string, error) {
    // エンドポイント
    endpoint := "https://acp-api-async.amivoice.com/v1/recognitions"
    if config.NoLog {
        endpoint = "https://acp-api-async.amivoice.com/v1/nolog/recognitions"
    }
    
    // 音声認識ジョブの開始
    sessionID, err := startAsyncJob(config, endpoint)
    if err != nil {
        return "", err
    }
    
    if config.Verbose {
        log.Printf("セッションID: %s", sessionID)
    }
    
    // ジョブの完了を待つ
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
    // マルチパートフォームの作成
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    
    // APP KEY
    writer.WriteField("u", config.AppKey)
    
    // dパラメータの構築
    dParams := buildDParams(config)
    writer.WriteField("d", dParams)
    
    // 音声フォーマット
    if config.AudioFormat != "" {
        writer.WriteField("c", config.AudioFormat)
    }
    
    // 音声ファイル
    file, err := os.Open(config.AudioFile)
    if err != nil {
        return "", fmt.Errorf("音声ファイルを開けません: %v", err)
    }
    defer file.Close()
    
    part, err := writer.CreateFormFile("a", filepath.Base(config.AudioFile))
    if err != nil {
        return "", err
    }
    
    _, err = io.Copy(part, file)
    if err != nil {
        return "", err
    }
    
    err = writer.Close()
    if err != nil {
        return "", err
    }
    
    // HTTPリクエストの作成と送信
    req, err := http.NewRequest("POST", endpoint, body)
    if err != nil {
        return "", err
    }
    req.Header.Set("Content-Type", writer.FormDataContentType())
    
    client := &http.Client{Timeout: 60 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    // レスポンスの読み取り
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }
    
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("HTTPエラー: %d - %s", resp.StatusCode, string(respBody))
    }
    
    // JSONのパース
    var jobResp AsyncJobResponse
    err = json.Unmarshal(respBody, &jobResp)
    if err != nil {
        return "", fmt.Errorf("JSONパースエラー: %v", err)
    }
    
    return jobResp.SessionID, nil
}

func checkAsyncJobStatus(config *Config, endpoint string, sessionID string) (string, string, error) {
    url := fmt.Sprintf("%s/%s", endpoint, sessionID)
    
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return "", "", err
    }
    req.Header.Set("Authorization", "Bearer "+config.AppKey)
    
    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()
    
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", "", err
    }
    
    var jobResp AsyncJobResponse
    err = json.Unmarshal(respBody, &jobResp)
    if err != nil {
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
    // WebSocket エンドポイント
    endpoint := "wss://acp-api.amivoice.com/v1/"
    if config.NoLog {
        endpoint = "wss://acp-api.amivoice.com/v1/nolog/"
    }
    
    // WebSocket接続
    dialer := websocket.Dialer{}
    conn, _, err := dialer.Dial(endpoint, nil)
    if err != nil {
        return "", fmt.Errorf("WebSocket接続エラー: %v", err)
    }
    defer conn.Close()
    
    // 認証コマンド
    authCmd := fmt.Sprintf("s %s", config.AppKey)
    err = conn.WriteMessage(websocket.TextMessage, []byte(authCmd))
    if err != nil {
        return "", err
    }
    
    // 認証応答を待つ
    _, msg, err := conn.ReadMessage()
    if err != nil {
        return "", err
    }
    
    if !strings.HasPrefix(string(msg), "s ") {
        return "", fmt.Errorf("認証エラー: %s", string(msg))
    }
    
    // 音声認識開始コマンド
    dParams := buildDParams(config)
    startCmd := fmt.Sprintf("s %s %s", config.AudioFormat, dParams)
    err = conn.WriteMessage(websocket.TextMessage, []byte(startCmd))
    if err != nil {
        return "", err
    }
    
    // 音声データの送信
    file, err := os.Open(config.AudioFile)
    if err != nil {
        return "", fmt.Errorf("音声ファイルを開けません: %v", err)
    }
    defer file.Close()
    
    // 音声データを小さなチャンクに分けて送信
    buffer := make([]byte, 4096)
    for {
        n, err := file.Read(buffer)
        if err == io.EOF {
            break
        }
        if err != nil {
            return "", err
        }
        
        // 'p'コマンドで音声データを送信
        err = conn.WriteMessage(websocket.BinaryMessage, append([]byte("p "), buffer[:n]...))
        if err != nil {
            return "", err
        }
    }
    
    // 音声終了コマンド
    err = conn.WriteMessage(websocket.TextMessage, []byte("e "))
    if err != nil {
        return "", err
    }
    
    // 結果の受信
    var results []string
    for {
        _, msg, err := conn.ReadMessage()
        if err != nil {
            return "", err
        }
        
        msgStr := string(msg)
        if config.Verbose {
            log.Printf("受信: %s", msgStr)
        }
        
        if strings.HasPrefix(msgStr, "A ") || strings.HasPrefix(msgStr, "U ") {
            // 認識結果
            parts := strings.SplitN(msgStr, " ", 2)
            if len(parts) > 1 {
                var result map[string]interface{}
                err := json.Unmarshal([]byte(parts[1]), &result)
                if err == nil {
                    if text, ok := result["text"].(string); ok && text != "" {
                        results = append(results, text)
                    }
                }
            }
        } else if strings.HasPrefix(msgStr, "e ") {
            // 終了
            break
        } else if strings.HasPrefix(msgStr, "E ") {
            // エラー
            return "", fmt.Errorf("WebSocketエラー: %s", msgStr)
        }
    }
    
    return strings.Join(results, "\n"), nil
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

func formatResponse(resp *SyncResponse, config *Config) string {
    if config.Verbose {
        // 詳細出力の場合はJSON形式
        jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
        return string(jsonBytes)
    }
    
    // 通常はテキストのみ
    return resp.Text
}