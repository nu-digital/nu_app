# WSL2で人が最初に行うセットアップ

対象は開発PC。VPSのOSインストールや本番secret設定ではない。現在のWindows cloneは残し、WSLのLinuxファイルシステムに別cloneを作る。同じ作業ツリーをWindowsとWSLから同時編集しない。

2026-11-20のこのPCの確認では `wsl --list --verbose` は「インストールされているディストリビューションはありません」。Ubuntu導入はまだ実施していない。

## 1. Teamの利用条件を確認（人/Team管理者）

[CLAUDE_TEAM](CLAUDE_TEAM.md)に沿って利用席、追加使用量無効、内包枠で選べるモデルを確認。Claude Codeの組織選択はTeam。APIキーを用意する必要はない。課金設定はエージェントに任せず管理画面で確認する。

## 2. WSL2とUbuntu（Windows管理者PowerShell）

```powershell
wsl --status
wsl --list --verbose
```

既存UbuntuがVERSION 2なら再インストール不要。未導入なら、[Microsoft公式手順](https://learn.microsoft.com/en-us/windows/wsl/install)を確認して実行する。

```powershell
wsl --install -d Ubuntu-24.04
```

必要な再起動後にUbuntuを開き、Linuxユーザーとパスワードを作る。既存ディストリビューションの変換・削除はこの手順で自動実行しない。24.04は開発環境の推奨で、本番Ubuntu版の確定ではない。

## 3. GitとGitHub認証（以降はUbuntuのBash）

```bash
sudo apt update
sudo apt install -y git curl ca-certificates build-essential gh
gh auth login --hostname github.com --git-protocol https --web
gh auth setup-git
gh auth status
mkdir -p ~/src
cd ~/src
git clone https://github.com/nu-digital/nu_app.git
cd nu_app
```

ブラウザーで人がGitHubログインし、対象repoへの権限を確認する。Windows側の一時認証スクリプトやtokenをコピーしない。`git config user.name` / `git config user.email`を確認し、未設定なら自分の名前とGitHubのnoreplyメール等をこのcloneに設定する。

準備PR #1のマージ後はmainを使用。マージ前の準備確認だけ行う場合は以下で準備ブランチへ切り替える。

```bash
git fetch origin
git switch --track origin/chore/bootstrap-agent-harness
```

実装開始時は最新mainからIssue番号付きブランチを作る。マージ前の準備ブランチで機能実装を始めない。

## 4. Linux版NodeとClaude Code

NodeはLinux側に導入する。バージョンマネージャーがなければ [nvm公式](https://github.com/nvm-sh/nvm#installing-and-updating) の導入手順を確認する。導入済みnvmなら:

```bash
nvm install "$(cat .node-version)"
nvm use "$(cat .node-version)"
node --version
node scripts/check.mjs
node --test scripts/harness.test.mjs
```

Claude Codeは [公式Linux/WSLインストーラー](https://code.claude.com/docs/en/setup) を使用する。人が公式URLを確認してから実行する。

```bash
curl -fsSL https://claude.ai/install.sh | bash
# 新しいUbuntuターミナルで確認
claude --version
claude doctor
command -v node
command -v claude
```

`node` / `claude`がWindowsの.exeや `/mnt/c/` の実行ファイルを指していないことを確認。インストーラーのPATH案内に従う。CLIは公式更新を使い、導入時と更新後の版を記録する。

起動前に、このBashに別の認証/モデル指定が混入していないか**変数名だけ**確認する。

```bash
compgen -e | grep -E '^(ANTHROPIC_|CLAUDE_CODE_USE_|CLAUDE_CODE_OAUTH_TOKEN)' || true
```

該当名があれば値をチャットへ貼らず人が設定元を調べる。APIキー、auth token、provider指定、endpoint、モデルalias上書きに加え、ユーザー/管理設定の `apiKeyHelper` も確認する。不要な設定の解除はそのシェル/設定元で行い、他プロジェクトの設定を一括削除しない。この一覧が空でもTeam認証を保証しない。

```bash
claude --model sonnet
```

初回はTeam所属のclaude.aiアカウントで人がブラウザーログインする。Claude内で `/status`、`/usage`、`/model`を確認し、組織・認証経路・使用量とモデルを確認する。`/memory`で指示読込、`/hooks`でStop hookを確認。課金に不明点があれば実装を開始しない。権限の全解除オプションは使わない。

## 5. Dockerとブラウザーテストの準備

DockerはP0のCompose作業前までに用意する。既にDocker Desktopを使っているなら [公式WSL2統合](https://docs.docker.com/desktop/features/wsl/) でUbuntuとの連携を有効にする。未導入なら [Docker EngineのUbuntu公式手順](https://docs.docker.com/engine/install/ubuntu/) を選べる。同じWSLに両方のdaemonを重ねて導入しない。

```bash
docker version
docker compose version
```

daemonへ接続できることを確認する。dockerグループ追加などのホスト権限変更は人が確認する。Go/sqlc/migration tool/PlaywrightはP0で採用版を決めてから導入し、今はlatestを無作為にインストールしない。ブラウザーのシステム依存を入れるsudo操作が必要なら人が対応する。

## 6. 引き渡しと再開

[CLAUDE_HANDOFF](CLAUDE_HANDOFF.md)を渡してIssue #2から開始する。通常のコード・テスト・ブランチpush・Draft PRはClaudeへ任せる。人は課金/ログイン/OS権限、未確定仕様、PRマージ、本番公開判断を担当する。

新しい `docs/handoffs/<issue番号>-progress.md` にOS、Node、CLI版、実際に確認したモデル、追加使用量無効の確認有無、テスト結果、未完了の環境項目を記録する。認証情報自体は記録しない。停止後は同じcloneで `claude --continue` または `claude --resume` を使う。

参考: [Linux側にプロジェクトを置く理由（Microsoft）](https://learn.microsoft.com/en-us/windows/wsl/filesystems)。この文書のコマンドはセットアップ手順であり、この準備でWSLへのインストールを実行済みではない。
