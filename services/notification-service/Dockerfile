# syntax=docker/dockerfile:1

FROM node:20-alpine

WORKDIR /app

# --chown=node:node で /app 配下の所有権をプリセットの node ユーザに移す。
# CIS Docker Benchmark 4.1 に従い、以降のプロセスは非 root で実行する。
COPY --chown=node:node package*.json ./
RUN npm ci

COPY --chown=node:node tsconfig.json .
COPY --chown=node:node src/ src/
RUN npm run build

# node:20-alpine にプリセットされている非 root ユーザで実行する。
USER node

EXPOSE 5003

CMD ["node", "dist/index.js"]
