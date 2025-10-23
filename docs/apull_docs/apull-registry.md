# 背景

基于apull容器镜像快速加载解决方案，超节点节点间使用高速网络通信，实现节点间镜像数据共享，减轻镜像仓库OBS桶网络带宽压力，实现更镜像拉取速度，进一步降低容器的冷启动时延。

# 实现思路

## 部署视图

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/WIKI202408224385061/11371795/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

## 时序图

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/WIKI202408224385061/11374085/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

## 运行视图

**初始化**

* 通过systemd管理apull-registry并使用普通用户权限运行apull-registry
* apull-registry加载apull-registry.toml配置文件
* apulld启动时配置apull-register的服务地址

**运行时**

1. 懒加载方式运行容器时apull接收到cache miss，优先向apull-registry请求镜像层数据
2. apull-registry会进行查询是否存在该镜像层数据，如果不存在则向镜像仓库数据请求镜像层数据；如果存在则将数据返回给对端
3. 镜像仓库返回数据，apull-registry会保存该镜像数据并返回给对端

## 配置说明

apulld新增配置如下：

```toml

[apulld.features]
# nodeshare whether to enable node share function
enable_registry_proxy = true

[apulld.registry]
tlsverify = true
certroot_dir = "to-your-dir"

[apulld.registry.proxy]
address="example.com"
tlsverify = true


```

apull-registry.toml配置如下

```toml
log_level = "info"
listen_address= "ip:port"
cache_dir = "cachePath"
# the certificate configuration required for two-way authentication
tlsverify = true
certroot_dir = "to-your-dir"
enable_registries_trustlist = true
registries_trustlist = [
        "example.com",
]
```

**配置说明**

- tlsverify
  如果为true，则配置项certroot_dir必须有值，且指向正确的根证书路径
- trusted_registries
  该配置指定apull-registry允许访问的registry白名单，只有在该白名单以内才允许访问，否则返回拒绝访问。当白名单配置为空时，允许所有请求访问客户端指定的registry。

## 镜像凭证方案

对于需要凭证（例如：账号密码）的镜像，apulld支持通过读取文件方式获取访问镜像仓库的账号密码。具体地说，apulld通过inotify机制监听凭证文件，若凭证文件存在写系统调用，则内核通知apulld更新重新读取该文件。apulld将凭证信息以map形式存储在内存中，apulld进程退出时信息丢失，启动时重新获取集群中最新凭证数据。

当内核向apulld发送镜像文件请求时，apulld首先会根据镜像仓库地址搜寻对应的凭证列表，并依次使用账号密码（若为密文存储，则先行解密）去获取可用token，或者全部账号均失效返回错误。若某一凭证成功，则apulld将保存该凭证，以便后续优先使用。

### 凭证格式

凭证文件为json文件，凭证以CBB加密或者明文格式存储。明文格式如下：

```json
{
  "auths": {
    "test_host": {
      "auth": [
        "user1:pass1",
        "user2:pass2"
      ]}}
}
```

其中，`test_host`为镜像仓库名，`auth`字段为字符串数组，允许同时存在多个账号密码。每一个凭证以`username:password`形式组合的字符串表示。密文格式如下：

```json
{
  "auths": {
    "test_host": {
      "auth": [   
       "0000000100000001339CB47449FD692CF7263B73A1412D024EC9C5478316F8BBE67D3E0307E7B530",
       "0000000100000001895E7996880E45D5511D44F6B93B9B0777B49953E1BB7C424BB02CFDC90FBD7A"
      ]}}
}
```

凭证以`encrypt(username:password)`的密文字符串表示。

## 使用方式

- 凭证文件
  
  用户通过apull-registry.toml配置文件指定凭证文件路径，即
  
  ```toml
  # /etc/apull/apull-registry.toml
  [apulld.registry.auths.file]
  	auth_path = "/usr/example.json"
  ```
  
  - 若用户未指定凭证文件，则默认凭证文件路径为`/etc/apull/credentials.json`。apull将会在`/etc/apull`目录下创建空文件`credentials.json`。
  - 在apulld生命周期内，禁止更改凭证文件所在目录的inode（例如删除目录后重建等），否则apulld无法获取最新凭证数据。
  - 在apull启动前，指定凭证文件所在的目录必须存在，否则apulld无法正确监听配置文件变化。
- 密文/明文
  
  用户可通过环境变量`EnableSecretEncrypt`和`PAAS_CRYPTO_PATH`控制apulld以密文或者明文形式使用凭证。
  
  ```bash
  # /usr/lib/systemd/system/apull-registry.service
  Environment="EnableSecretEncrypt=true"
  Environment="PAAS_CRYPTO_PATH=/opt/cloud/cce/srv/kubernetes"
  ```
  
  - `EnableSecretEncrypt`表示凭证是否加密存储。"true"表示凭证为密文，需要解密使用。
  - `PAAS_CRYPTO_PATH`表示密钥根目录，默认为当前目录。密钥由用户提供，需与用户加密密钥目录相同。
