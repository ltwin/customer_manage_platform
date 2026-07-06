# 多阶段构建（design D6/D7）：前端产物 go:embed 进单一 Go 二进制，镜像里只有一个二进制。
# 凭证一律运行期经环境注入，构建产物与镜像不含任何凭证。

FROM node:24-alpine AS webui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
COPY --from=webui /src/frontend/dist ./backend/internal/platform/webui/dist
RUN cd backend && CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D app
USER app
COPY --from=backend /out/server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
