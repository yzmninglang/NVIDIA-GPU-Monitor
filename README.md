# 分布式NVIDIA GPU监控系统

这是一个用于监控分布式系统中多个节点GPU使用情况的监控系统。系统由两部分组成：
1. 服务端（Server）：运行在每个GPU节点上，收集并提供该节点的GPU信息
2. 中心聚合端（Aggregator）：收集所有节点的信息并在Web界面中展示

<img width="1526" height="832" alt="image" src="https://github.com/user-attachments/assets/d4d7639d-5cf6-4ab0-897d-451a722b18ce" />

## 功能特性

- 实时监控多个节点的GPU使用情况
- 显示GPU利用率、显存占用、温度、功耗等信息
- 显示使用GPU的进程信息，按显存占用排序
- 节点离线检测和状态显示
- 响应式Web界面
- 支持通过配置文件定义监控节点
- 支持自定义DNS服务器解析本地域名
- Web界面显示节点IP地址
- 支持多种架构（x86_64和ARM64）

## 安装和部署

### 依赖

- Go 1.16+
- NVIDIA驱动（用于实际GPU信息收集）

### 构建

```bash
# 构建程序
go build -o gpu-monitor

# 或者构建静态链接版本（避免GLIBC兼容性问题）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o gpu-monitor

# 构建ARM64版本
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -o gpu-monitor-arm64
```

## 使用方法

### 运行模式

程序支持两种运行模式，通过`-mode`参数指定：

1. 服务端模式（在GPU节点上运行）：
```bash
./gpu-monitor -mode=server -port=8081
```

2. 聚合端模式（在中心节点上运行）：
```bash
./gpu-monitor -mode=aggregator -config=config.json -port=8080
```

### 配置文件

创建一个`config.json`文件来定义监控的节点：

```json
{
  "nodes": [
    {
      "name": "gpu-server-1",
      "host": "192.168.1.101",
      "port": 8081,
      "alias": "机器学习服务器"
    },
    {
      "name": "gpu-server-2",
      "host": "gpu-server.local",
      "port": 8081,
      "alias": "深度学习服务器"
    }
  ],
  "aggregator": {
    "port": 8080
  },
  "dns": {
    "server": "127.0.0.1:5353",
    "enabled": true
  }
}
```

### 命令行参数

- `-mode`：运行模式，可选`server`或`aggregator`，默认为`aggregator`
- `-port`：监听端口，会覆盖配置文件中的端口设置
- `-config`：配置文件路径，默认为`config.json`

## API接口

### 服务端接口

- `GET /gpu-info`：获取GPU信息
- `GET /health`：健康检查

### 聚合端接口

- `GET /api/nodes`：获取所有节点的状态信息（按配置文件顺序返回）
- `GET /api/nodes/{name}`：获取特定节点的详细信息
- `GET /`：Web界面

## Web界面

聚合端提供一个Web界面来展示所有节点的GPU信息。界面会每5秒自动刷新一次。

界面显示内容：
- 节点状态（在线/离线）
- 节点别名和IP地址（绿色加粗字体）
- 每个GPU的详细信息（利用率、显存、温度、功耗）
- 使用GPU的进程信息（按显存占用降序排列）
- 最后更新时间

## 架构说明

```
+------------------+     +------------------+     +------------------+
|   GPU Node 1     |     |   GPU Node 2     |     |   GPU Node N     |
|                  |     |                  |     |                  |
|  +------------+  |     |  +------------+  |     |  +------------+  |
|  | GPU Server |<-------+->| GPU Server |<-------+->| GPU Server |  |
|  +------------+  |     |  +------------+  |     |  +------------+  |
+------------------+     +------------------+     +------------------+
         ^                        ^                        ^
         |                        |                        |
         |                    HTTP/JSON                  |
         +------------------------+------------------------+
                                  |
                                  v
                      +-----------------------+
                      |  Aggregator Server    |
                      |                       |
                      |  +-----------------+  |
                      |  |   Web Interface |  |
                      |  +-----------------+  |
                      +-----------------------+
```

## 注意事项

1. 在生产环境中，请确保防火墙允许相应端口的通信
2. 服务端需要能够访问NVIDIA驱动和GPU设备
3. 配置文件中的节点地址需要确保聚合端可以访问
4. 可以根据需要调整轮询间隔和其他参数

## 故障排除

- 如果节点显示为离线，请检查：
  - 节点服务是否正常运行
  - 网络连接是否正常
  - 防火墙设置是否正确
  - 端口配置是否正确
  - 主机名解析是否正确（见下面的DNS配置说明）

- 如果GPU信息不正确显示，请检查：
  - NVIDIA驱动是否正确安装
  - 服务是否有权限访问GPU设备

## DNS配置说明

当使用主机名（而不是IP地址）配置节点时，需要确保聚合端服务器能够解析这些主机名。

### 自定义DNS服务器

系统支持配置自定义DNS服务器来解析本地域名。在配置文件中添加`dns`部分：

```json
{
  "dns": {
    "server": "127.0.0.1:5353",
    "enabled": true
  }
}
```

如果启用了自定义DNS服务器，系统会优先使用该服务器来解析主机名。这对于使用Avahi服务的本地网络特别有用。

### 本地域名解析

如果使用本地域名（如`gpu-server.local`），需要在DNS服务器上配置相应的记录，或者在聚合端服务器的`/etc/hosts`文件中添加映射：

```bash
# 编辑 /etc/hosts 文件
sudo nano /etc/hosts

# 添加类似以下的行：
192.168.1.100 gpu-server.local
192.168.1.101 lab108-5090.local
```

### 测试域名解析

可以使用以下命令测试域名解析是否正常：

```bash
# 测试域名解析
nslookup gpu-server.local
ping gpu-server.local
```

### 使用IP地址避免DNS问题

为了避免DNS解析问题，建议在配置文件中直接使用IP地址而不是主机名：

```json
{
  "nodes": [
    {
      "name": "gpu-server-1",
      "host": "192.168.1.101",  // 使用IP地址而不是主机名
      "port": 8081,
      "alias": "机器学习服务器"
    }
  ],
  "aggregator": {
    "port": 8080
  }
}
```

## 版本和兼容性

### GLIBC兼容性

为了解决不同Linux发行版之间的GLIBC版本兼容性问题，我们提供了静态链接版本的二进制文件：
- `gpu-monitor`：静态链接版本，适用于大多数Linux发行版
- `gpu-monitor-arm64`：ARM64架构的静态链接版本

### GPU兼容性

系统已测试支持以下NVIDIA GPU：
- RTX 5090
- RTX 3090
- RTX 3080
- RTX 3070

其他支持nvidia-smi命令的NVIDIA GPU也应该可以正常工作。
