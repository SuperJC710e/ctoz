# 多阶段构建
# 阶段1: 构建前端
FROM node:18-alpine AS frontend-builder

WORKDIR /app

# 复制前端依赖文件
COPY package*.json ./
COPY tsconfig*.json ./
COPY vite.config.ts ./
COPY tailwind.config.js ./
COPY postcss.config.js ./

# 安装前端依赖
RUN npm ci

# 复制前端源码
COPY frontend/ ./frontend/

# 构建前端
RUN npm run build

# 阶段2: 构建后端
FROM golang:1.21-alpine AS backend-builder

WORKDIR /app

# 安装必要的包
RUN apk add --no-cache git

# 复制Go模块文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制后端源码
COPY backend/ ./backend/

# 构建后端
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./backend/cmd

# 阶段3: 最终镜像
FROM alpine:latest

WORKDIR /app

# 安装必要的包
RUN apk --no-cache add ca-certificates tzdata zip unzip

# 设置时区
RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
RUN echo 'Asia/Shanghai' >/etc/timezone

# 从构建阶段复制文件
COPY --from=backend-builder /app/main .
COPY --from=frontend-builder /app/dist ./frontend/dist

# 暴露端口
EXPOSE 8080

# 运行应用
CMD ["./main"]