# Ubuntu VPS運用の実装前契約

ユーザー指定: UbuntuのVPS。要件上は4GBの単一ホスト、Docker Compose、本番専用。正確なUbuntu版、CPUアーキテクチャ、プロバイダー、SSH接続先、ドメイン取得状態は未確認。

## P0で用意するもの

- 開発用Composeと架空値の `.env.example`。DBポートを公開するならloopbackだけにbindする。
- Next.js web/adminとGo API/Botの責任境界。バージョン・lockfile・採用理由を固定。
- Linux CIでビルド・テスト。ローカル環境との差（パス大文字小文字、LF、実行権限）も確認。

## 本番に接続する前に決めるもの

Ubuntu LTSの版とCPUアーキテクチャ、Nginx採用、DNS/TLS、デプロイ権限、secret配置、バックアップ頻度/保持、メディア上限、Owner復旧手順を [DECISIONS](DECISIONS.md) で解決する。未入力の値を仮定してSSH接続やDNS変更をしない。

## デプロイ設計の合格条件

- mainマージ後のGitHub Actionsでテスト済みSHAのイメージをビルド・push。VPSではビルドしない。PRに本番secretを渡さない。
- 本番デプロイは直列化。途中で新デプロイに中断されてmigrationだけ進む状態を避ける。
- 前版SHAを保存し、DB変更の互換性確認後にmigrationと更新を行う。livenessとDBを含むreadinessを区別し、失敗時は成功通知を送らない。
- DB/API/Botを直接インターネットに公開しない。HTTPS、最小権限ユーザー、ホスト鍵検証、Docker権限、Firewallを確認する。
- secretはGit外。実token・招待URL・学生情報をログや通知に含めない。
- DBとメディアはコンテナ外の永続領域。`docker compose down -v`を本番運用に使わない。
- 切り戻しもレビューされたGitHub Actions経由。前版イメージ復帰はDBの完全巻き戻しではない。破壊的schema変更は段階移行する。
- ローカルバックアップはDBとメディアの整合性を考慮し、隔離環境で復元して確認する。VPS外バックアップは初期スコープ外であり、VPS喪失時はデータを失う可能性がある。
- 4GB上で全サービスのメモリ/CPU/ディスク、ログ増加、バックアップ容量を実測する。開発サーバーは常駐させない。

この準備では本番ワークフローを有効化していない。初回公開前に `docs/TESTING.md` のP4と管理者手順を満たす。
