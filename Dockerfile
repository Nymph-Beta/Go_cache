# 使用 Ubuntu 20.04 作为基础镜像
FROM ubuntu:20.04

# 使用非交互式前端，避免在构建过程中等待用户输入
ENV DEBIAN_FRONTEND=noninteractive

# 安装 tzdata 以及其他必需的包
RUN apt-get update && \
    apt-get install -y tzdata golang-go && \
    ln -fs /usr/share/zoneinfo/UTC /etc/localtime && \
    dpkg-reconfigure --frontend noninteractive tzdata

# 安装 Go
RUN apt-get update && apt-get install -y golang-go

# 设置工作目录
WORKDIR /app

# 复制所有源代码到容器中的 /app 目录
COPY . /app

# 编译 Go 程序，例如 main.go
#RUN go build -o myapp main.go
RUN go build -o geecache_serve

# 更改 geecache 文件的权限，使其可执行
RUN chmod +x geecache_serve

# 设置容器启动时运行的命令
CMD ["./geecache_serve"]
