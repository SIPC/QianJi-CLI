# 钱迹 CLI 的 MCP server 容器镜像；配置与缓存持久化到挂载卷 /data。
#   docker build -t qianji-mcp .
#   # 先起 server（未登录也不崩）：
#   docker run -d --name qianji -p 8787:8787 -v qianji-data:/data -e QIANJI_MCP_PATH=密钥 qianji-mcp
#   # 再登录（写入卷，下次调用即生效）：
#   docker exec -it -e QIANJI_PASSWORD=密码 qianji qianji login --account 账号
# MCP 端点： POST http://<host>:8787/mcp/<QIANJI_MCP_PATH>
# 备选：不挂卷，直接 -e QIANJI_UID=<uid> -e QIANJI_TOKEN=<token> 注入登录态。

# --- build ---
FROM golang:1.26 AS build
WORKDIR /src
COPY cli/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /qianji .

# --- runtime (distroless: 无 shell、体积小、较安全) ---
FROM gcr.io/distroless/static-debian12
COPY --from=build /qianji /usr/local/bin/qianji
# 配置 + 账单/分类缓存都落在 /data，挂卷即持久化
ENV QIANJI_CONFIG_DIR=/data
VOLUME ["/data"]
EXPOSE 8787
# entrypoint 拆开，便于 `docker run ... login ...` 覆盖默认的 mcp 命令
ENTRYPOINT ["qianji"]
CMD ["mcp", "--addr", ":8787"]
