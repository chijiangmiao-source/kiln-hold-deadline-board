# 前端容器：保留源码与依赖，既能托管页面也能进入容器运行测试。
FROM node:20-alpine
WORKDIR /app
RUN chown node:node /app
COPY --chown=node:node package.json package-lock.json ./
USER node
RUN npm ci
COPY --chown=node:node . .
RUN npm run build
# compose 网络内 API 服务名为 api；本地单跑容器时可用 -e API_TARGET=... 覆盖。
ENV API_TARGET=http://api:8080
EXPOSE 5173
CMD ["npx", "vite", "preview", "--host", "0.0.0.0", "--port", "5173"]
