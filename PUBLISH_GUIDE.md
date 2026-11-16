# GitHub 公開手順（木下さん用メモ）

リポジトリ名のおすすめ: `imagemetahub-ja-patch`

## 1. リポジトリを作る

1. GitHub にログイン → 右上の「+」→「New repository」
2. Repository name: `imagemetahub-ja-patch`
3. Public のまま →「Create repository」
4. README 等は追加しない（こちらで用意したものを上げるため）

## 2. ファイルを上げる（Web画面からでOK）

1. 作ったリポジトリの画面で「uploading an existing file」をクリック
   （または Add file → Upload files）
2. `github-repo` フォルダの中身をドラッグ＆ドロップ
   - フォルダごとではなく「中身」を選ぶのがコツです
   - `patcher` フォルダはフォルダごとドラッグできます
3. 下の「Commit changes」を押す

上がるファイルはこれだけです:

- `README.md` / `MAINTAINING.md` / `LICENSE` / `.gitignore`
- `patcher/main.go` / `main_windows.go` / `main_other.go` / `go.mod` / `dict.json` / `build.bat`

※ exe はここには上げません（次の Release で添付します）。
※ `.gitignore` が exe や app.asar を弾く設定になっているので、間違えて本体を上げてしまう事故も起きません。

## 3. Release を作る（exe の配布場所）

1. リポジトリ右側の「Releases」→「Create a new release」
2. Tag: `v1.0.0`（入力すると「Create new tag」と出るのでそれを選ぶ）
3. Release title と本文: `RELEASE_NOTES_v1.0.0.md` のドラフトをそのまま貼る
4. 下の「Attach binaries by dropping them here」に、手元の `imagemetahub-ja-patch.exe` をドラッグ
5. 「Publish release」

## 4. 公開前の最終確認

- [ ] 自分の PC でパッチを実行して、Image MetaHub が日本語で起動すること
- [ ] 復元（もう一度実行して `y`）で英語に戻ること
- [ ] README の表示が崩れていないこと（リポジトリのトップで確認）

この2つが OK なら公開して大丈夫です。

## 名前について

- `LICENSE` の著作権者名は今は `imagemetahub-ja-patch contributors` と仮置きしています。
  自分のハンドル名で出したければ、そこの1行を書き換えてください（本名である必要はありません）。
