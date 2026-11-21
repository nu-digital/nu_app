# syntax=docker/dockerfile:1
# Go・ベースイメージは固定(docs/TOOLCHAIN.md)。本番VPSではビルドせず、CIでビルドしたイメージを使う。
FROM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN mkdir -p /out/data/media && chown -R 65532:65532 /out/data
RUN CGO_ENABLED=0 GOOS=linux go build -tags nodynamic -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -tags nodynamic -trimpath -ldflags="-s -w" -o /out/nuctl ./cmd/nuctl

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/server /server
# 管理CLI（運用者がサーバー上で実行する。公開経路からは呼ばれない）
COPY --from=build /out/nuctl /nuctl
# 画像の保存先（永続ボリュームをここへマウントする。空のディレクトリをnonrootの所有で用意し、名前付きボリュームの初期の所有者を合わせる）
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=4s --start-period=5s --retries=3 CMD ["/server", "-healthcheck"]
ENTRYPOINT ["/server"]
