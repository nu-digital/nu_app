# syntax=docker/dockerfile:1
# migration実行用イメージ。gooseは固定版(docs/TOOLCHAIN.md)。
FROM golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
RUN CGO_ENABLED=0 GOOS=linux go install -trimpath -ldflags="-s -w" github.com/pressly/goose/v3/cmd/goose@v3.30.1

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /go/bin/goose /goose
COPY migrations /migrations
USER nonroot:nonroot
ENTRYPOINT ["/goose", "-dir", "/migrations", "postgres"]
