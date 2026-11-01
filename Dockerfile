# 构建钱迹 CLI 的 MCP server 容器镜像。
#   docker build -t qianji-mcp .
#   docker run -d -p 8787:8787 \
#     -e QIANJI_UID=<你的uid> -e QIANJI_TOKEN=<你的token> \
#     -e QIANJI_MCP_PATH=<一长串随机密钥> qianji-mcp
# MCP 端点： POST http://<host>:8787/mcp/<QIANJI_MCP_PATH>

# --- build ---
FROM golang:1.26 AS build
WORKDIR /src
COPY cli/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /qianji .

# --- runtime (distroless: 无 shell、体积小、较安全) ---
FROM gcr.io/distroless/static-debian12
COPY --from=build /qianji /usr/local/bin/qianji
EXPOSE 8787
# 登录态用环境变量注入（QIANJI_UID / QIANJI_TOKEN），密钥路径用 QIANJI_MCP_PATH。
ENTRYPOINT ["qianji", "mcp", "--addr", ":8787"]
