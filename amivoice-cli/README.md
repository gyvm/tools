# AmiVoice API CLI Tool for Go

AmiVoice APIを使用して音声認識を行うGo言語製のCLIツールです。同期HTTPインタフェース、非同期HTTPインタフェース、WebSocketインタフェースの3つに対応しています。

## 必要な環境

- Go 1.16以上
- AmiVoice API APP KEY

## インストール

```bash
# 依存関係のインストール
go mod init amivoice-cli
go get github.com/gorilla/websocket

# ビルド
go build -o amivoice-cli main.go

# 環境変数の設定（tools/ ディレクトリ全体で共通）
cp ../.env.example ../.env
# ../.envファイルを編集してAPP KEYを設定
```

## 使い方

### 基本的な使い方

```bash
# 同期HTTPインタフェース（デフォルト）
./amivoice-cli -appkey YOUR_APP_KEY -file audio.wav

# 環境変数でAPP KEYを設定（方法1: export）
export AMIVOICE_APP_KEY=YOUR_APP_KEY
./amivoice-cli -file audio.wav

# 環境変数でAPP KEYを設定（方法2: .envファイル）
# ../.envファイルにAMIVOICE_APP_KEY=YOUR_APP_KEYを設定
./amivoice-cli -file audio.wav
```

### インタフェースの選択

```bash
# 同期HTTPインタフェース（16MB以下のファイル）
./amivoice-cli -interface sync -file audio.wav

# 非同期HTTPインタフェース（大きなファイル）
./amivoice-cli -interface async -file large_audio.wav

# WebSocketインタフェース（リアルタイム処理）
./amivoice-cli -interface websocket -file audio.wav
```

### オプション一覧

| オプション | 説明 | デフォルト |
|----------|------|----------|
| `-appkey` | AmiVoice API APP KEY | 環境変数から取得 |
| `-interface` | インタフェース (sync/async/websocket) | sync |
| `-engine` | 音声認識エンジン | -a-general |
| `-file` | 音声ファイルパス（必須） | - |
| `-format` | 音声フォーマット (例: LSB16K, LSB8K) | 自動判定 |
| `-samplerate` | サンプリングレート | - |
| `-profileid` | プロファイルID | - |
| `-profilewords` | プロファイル単語 | - |
| `-keepfiller` | フィラー単語を保持 (1で有効) | - |
| `-loosesymbol` | 記号を緩く扱う (1で有効) | - |
| `-resulttype` | 結果タイプ | - |
| `-nolog` | ログ保存なし | false |
| `-verbose` | 詳細出力（JSON形式） | false |
| `-output` | 出力ファイルパス | 標準出力 |
| `-polling` | 非同期処理のポーリング間隔（秒） | 5 |

### 音声フォーマットの指定

ヘッダー付き音声ファイル（WAV、FLAC等）の場合：
```bash
./amivoice-cli -file audio.wav
```

ヘッダーなしPCMデータの場合：
```bash
# 16kHz, 16bit, リトルエンディアン
./amivoice-cli -file audio.pcm -format LSB16K

# 8kHz, 16bit, リトルエンディアン
./amivoice-cli -file audio.pcm -format LSB8K
```

### 詳細な使用例

1. **会話音声の認識（詳細出力）**
```bash
./amivoice-cli -file meeting.wav -verbose -output result.json
```

2. **カスタム辞書を使用した認識**
```bash
./amivoice-cli -file audio.wav -profilewords "www とりぷるだぶる"
```

3. **大きなファイルの非同期処理**
```bash
./amivoice-cli -interface async -file large_meeting.wav -polling 10
```

4. **ログを保存しない認識**
```bash
./amivoice-cli -file audio.wav -nolog
```

5. **フィラー単語を含めた認識**
```bash
./amivoice-cli -file audio.wav -keepfiller 1 -verbose
```

## サポートされている音声形式

- WAV, FLAC, Ogg等のヘッダー付きファイル
- PCM生データ（フォーマット指定必須）
- サンプリングレート: 8kHz, 11.025kHz, 16kHz, 22.05kHz, 32kHz, 44.1kHz, 48kHz
- 量子化ビット数: 16bit
- チャンネル数: モノラル（ステレオファイルの場合は第1チャンネルのみ使用）

## エラー処理

- APP KEYが設定されていない場合はエラー終了
- 音声ファイルが見つからない場合はエラー終了
- API認証エラーの場合は詳細メッセージを表示
- ネットワークエラーの場合はタイムアウト後にエラー終了

## 制限事項

- 同期HTTPインタフェース: 音声ファイルサイズ16MB以下
- 非同期HTTPインタフェース: 処理完了まで時間がかかる（音声長の0.5〜1.5倍）
- WebSocketインタフェース: 現在の実装では音声ファイル全体を送信（ストリーミング非対応）

## トラブルシューティング

### 認証エラーが発生する場合
- APP KEYが正しいか確認
- 環境変数 `AMIVOICE_APP_KEY` が設定されているか確認
- `../.env` ファイルが存在し、正しく設定されているか確認

### 音声認識結果が空の場合
- 音声フォーマットが正しいか確認
- 音声ファイルに発話が含まれているか確認
- `-verbose` オプションで詳細情報を確認

### タイムアウトエラーが発生する場合
- ネットワーク接続を確認
- 非同期インタフェースの場合は `-polling` 間隔を長くする

## ファイル構成

```
tools/
├── .env.example     # 環境変数テンプレート（全ツール共通）
├── .env             # 環境変数設定（全ツール共通、gitignoreに追加推奨）
└── amivoice-cli/
    ├── main.go      # メインプログラム
    ├── go.mod       # Go モジュール設定
    ├── README.md    # このファイル
    └── .gitignore   # Git除外設定
```

## 環境変数設定

`../.env` ファイル（tools/ ディレクトリの.env）で以下の環境変数を設定できます：

```bash
# 必須
AMIVOICE_APP_KEY=your_app_key_here

# オプション（コマンドライン引数で上書き可能）
AMIVOICE_ENGINE=-a-general
AMIVOICE_AUDIO_FORMAT=LSB16K
AMIVOICE_INTERFACE=sync
AMIVOICE_VERBOSE=false
AMIVOICE_NOLOG=false
AMIVOICE_POLLING_INTERVAL=5
```

## ライセンス

このツールはサンプルコードとして提供されています。AmiVoice APIの利用には別途利用規約が適用されます。