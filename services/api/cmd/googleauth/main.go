// googleauth は、会計機能（Google Drive・Sheets連携）用のOAuthリフレッシュトークンを取得する、初回認可用のコマンド。
// 運用責任者が自分のPC（ブラウザのある環境）で1回だけ実行する。VPSでは実行しない。
//
// 使い方（docs/design/phase3-accounting.md §12）:
//
//	GOOGLE_OAUTH_CLIENT_ID=... GOOGLE_OAUTH_CLIENT_SECRET=... go run ./cmd/googleauth [-folder <フォルダID>] [-create-root <名前>]
//
// クライアントIDとシークレットは環境変数で渡す（引数・ログに残さない）。取得したリフレッシュトークンは標準出力に1回だけ表示する
// （VPSの.envへ手で貼る。GitHub・チャット・ログに貼らない）。外部ライブラリは使わず、標準ライブラリだけで動く（PKCE・state・ループバック）。
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	authURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenURL = "https://oauth2.googleapis.com/token"
	driveAPI = "https://www.googleapis.com/drive/v3"
)

// 要求するスコープ（最小）。drive.file＝このアプリが作成した（または明示的に開いた）ファイルだけ。spreadsheets＝既存の台帳（スプレッドシート）への書き込み。
var scopes = []string{"https://www.googleapis.com/auth/drive.file", "https://www.googleapis.com/auth/spreadsheets"}

func main() {
	folder := flag.String("folder", "", "アクセスできるか確認するDriveのフォルダID（任意。読み取りだけ）")
	createRoot := flag.String("create-root", "", "このアプリ用のルートフォルダをマイドライブ直下に作る（名前。任意）")
	flag.Parse()
	id, secret := os.Getenv("GOOGLE_OAUTH_CLIENT_ID"), os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")
	if id == "" || secret == "" {
		fmt.Fprintln(os.Stderr, "環境変数 GOOGLE_OAUTH_CLIENT_ID と GOOGLE_OAUTH_CLIENT_SECRET を設定してください")
		os.Exit(2)
	}
	if err := run(id, secret, *folder, *createRoot); err != nil {
		fmt.Fprintln(os.Stderr, "失敗:", err)
		os.Exit(1)
	}
}

func run(clientID, clientSecret, folder, createRoot string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0") // 自分のPCだけで受ける（外部に公開しない）
	if err != nil {
		return err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)
	verifier, challenge := pkce()
	state := randomString(24)

	fmt.Println("次のURLをブラウザで開き、**団体公式のGmailアカウント**でログインして許可してください:")
	fmt.Println()
	fmt.Println(AuthorizeURL(clientID, redirect, state, challenge))
	fmt.Println()
	fmt.Println("（「このアプリはGoogleで確認されていません」と出たら、自分たちのアプリなので「詳細」→「続行」。待機は5分）")

	code, err := waitForCode(ctx, ln, state)
	if err != nil {
		return err
	}
	tok, err := exchange(ctx, clientID, clientSecret, redirect, code, verifier)
	if err != nil {
		return err
	}
	if err := checkScopes(tok.Scope); err != nil {
		return err
	}
	if tok.RefreshToken == "" {
		return errors.New("リフレッシュトークンが返りませんでした。Googleアカウントの「サードパーティのアクセス」でこのアプリを一度削除してから、やり直してください")
	}
	if folder != "" {
		checkFolder(ctx, tok.AccessToken, folder)
	}
	if createRoot != "" {
		if err := createRootFolder(ctx, tok.AccessToken, createRoot); err != nil {
			return err
		}
	}
	fmt.Println()
	fmt.Println("成功しました。次の値をVPSの .env に設定してください（この画面以外に残さない）:")
	fmt.Println("GOOGLE_OAUTH_REFRESH_TOKEN=" + tok.RefreshToken)
	return nil
}

// AuthorizeURL は認可画面のURL（オフライン＝リフレッシュトークンを得る。毎回同意を求め、確実にトークンを返させる）。
func AuthorizeURL(clientID, redirect, state, challenge string) string {
	q := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {strings.Join(scopes, " ")}, "access_type": {"offline"}, "prompt": {"consent"},
		"state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	return authURL + "?" + q.Encode()
}

func pkce() (verifier, challenge string) {
	verifier = randomString(48)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func waitForCode(ctx context.Context, ln net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var res result
		switch {
		case q.Get("state") != state: // 別のリクエスト（CSRF）を弾く
			res.err = errors.New("stateが一致しません。やり直してください")
		case q.Get("error") != "":
			res.err = fmt.Errorf("認可が拒否されました: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("codeがありません")
		default:
			res.code = q.Get("code")
		}
		if res.err != nil {
			http.Error(w, "失敗しました。ターミナルを確認してください", http.StatusBadRequest)
		} else {
			fmt.Fprintln(w, "認可できました。このタブを閉じて、ターミナルに戻ってください。")
		}
		select {
		case ch <- res:
		default:
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	select {
	case r := <-ch:
		return r.code, r.err
	case <-ctx.Done():
		return "", errors.New("時間切れです")
	}
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func exchange(ctx context.Context, id, secret, redirect, code, verifier string) (tokenResp, error) {
	form := url.Values{"client_id": {id}, "client_secret": {secret}, "code": {code}, "redirect_uri": {redirect}, "grant_type": {"authorization_code"}, "code_verifier": {verifier}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var t tokenResp
	if err := doJSON(req, &t); err != nil {
		return t, err
	}
	if t.Error != "" || t.AccessToken == "" {
		return t, fmt.Errorf("トークンの交換に失敗しました（%s）", t.Error)
	}
	return t, nil
}

// checkScopes は、許可された範囲に必要なスコープがそろっているか確認する（画面で一部だけ許可された場合を検出）。
func checkScopes(granted string) error {
	have := map[string]bool{}
	for _, s := range strings.Fields(granted) {
		have[s] = true
	}
	for _, s := range scopes {
		if !have[s] {
			return fmt.Errorf("必要な権限が許可されていません: %s（認可画面で、すべてのチェックを入れてください）", s)
		}
	}
	return nil
}

func doJSON(req *http.Request, out any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(b, out)
	return nil
}

// checkFolder は、drive.fileの範囲で、既存のフォルダにアクセスできるか確かめる（読み取りだけ）。
// drive.fileは「このアプリが作ったファイル」しか扱えないため、人が作ったフォルダは見えない（404）可能性がある。結果で、ルートの作り方を決める。
func checkFolder(ctx context.Context, access, id string) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, driveAPI+"/files/"+url.PathEscape(id)+"?fields=id,name,mimeType&supportsAllDrives=true", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("フォルダの確認に失敗:", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("フォルダ確認: HTTP %d（200＝このアプリから見える／404＝見えない。404ならdrive.fileでは既存フォルダ配下に作れないので、-create-root で専用のルートを作る）\n", resp.StatusCode)
}

func createRootFolder(ctx context.Context, access, name string) error {
	body, _ := json.Marshal(map[string]any{"name": name, "mimeType": "application/vnd.google-apps.folder"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, driveAPI+"/files?fields=id,name", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	var out struct{ ID, Name string }
	if err := doJSON(req, &out); err != nil || out.ID == "" {
		return errors.New("ルートフォルダを作れませんでした")
	}
	fmt.Printf("ルートフォルダを作成しました（名前: %s）。VPSの .env に DRIVE_RECEIPTS_ROOT_FOLDER_ID=%s を設定してください。\n（DriveのUIで、好きな場所へ移動・名前変更してかまいません。IDは変わりません）\n", out.Name, out.ID)
	return nil
}
