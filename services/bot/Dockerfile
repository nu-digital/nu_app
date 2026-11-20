# syntax=docker/dockerfile:1
# Go・ベースイメージは固定(docs/TOOLCHAIN.md)。本番VPSではビルドせず、CIでビルドしたイメージを使う。
FROM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/bot /bot
USER nonroot:nonroot
ENTRYPOINT ["/bot"]
