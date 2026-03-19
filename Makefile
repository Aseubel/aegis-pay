.PHONY: all build run test clean docker-build docker-up docker-down docker-logs wire wire-check

# 默认目标
all: build

# 编译应用
build:
	go build -o aegis-pay.exe ./cmd/server

# 本地运行（需要先启动数据库）
run:
	go run ./cmd/server

# 运行测试
test:
	go test -v ./...

# 清理构建产物
clean:
	rm -f aegis-pay.exe
	go clean

# 生成 Wire 依赖注入代码
wire:
	go run github.com/google/wire/cmd/wire@latest gen ./cmd/server

# 检查 Wire 生成的代码是否是最新的
wire-check:
	go run github.com/google/wire/cmd/wire@latest check ./cmd/server

# 构建 Docker 镜像
docker-build:
	docker build -t aegis-pay:latest .

# 启动基础设施（MySQL + Redis）
docker-up:
	docker-compose up -d

# 停止基础设施
docker-down:
	docker-compose down

# 查看基础设施日志
docker-logs:
	docker-compose logs -f

# 构建并启动全部服务（包括应用）
docker-all: docker-up docker-build
	docker run -d --name aegis-pay \
		--link aegis-mysql \
		--link aegis-redis \
		-p 8080:8080 \
		-e MYSQL_HOST=aegis-mysql \
		-e REDIS_HOST=aegis-redis \
		aegis-pay:latest

# 停止并清理全部服务
docker-clean: docker-down
	docker stop aegis-pay 2>/dev/null || true
	docker rm aegis-pay 2>/dev/null || true
	docker rmi aegis-pay:latest 2>/dev/null || true

# 初始化数据库
init-db:
	docker exec -it aegis-mysql mysql -uroot -proot -e "CREATE DATABASE IF NOT EXISTS aegis_pay;"

# 查看应用日志
logs:
	docker logs -f aegis-pay

# 进入 MySQL
mysql:
	docker exec -it aegis-mysql mysql -uroot -proot aegis_pay

# 进入 Redis
redis:
	docker exec -it aegis-redis redis-cli
