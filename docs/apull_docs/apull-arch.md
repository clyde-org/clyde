# Introduction

apull是强大高效的容器镜像快速加载解决方案。它基于内核erofs文件系统和fscache实现bootstrap(空文件系统)加载，以及在用户态apulld中处理内核blob数据(文件数据)请求实现容器镜像懒加载和快速启动。该方案提升了容器启动速度、提升网络带宽效率、减少镜像空间占用以及校验数据完整性，为云原生工作负载容器镜像等提供高效快速的分发和启动能力。

该特性主要涉及用户态两个模块：

- apull-snapshotter: 支持erofs文件系统挂载的containerd snapshotter插件
- apulld: 处理fscache事件请求，并从容器镜像服务器拉取容器镜像数据

# apull容器镜像懒加载原理

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T2037Z/de13e817281b406ca2cc39b213117cff/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

1. containerd通过加载oci-ref镜像(bootstrap)，实现拉起容器镜像rootfs。oci-ref镜像是文件系统的引导数据，只包含文件系统的结构和排列关系，没有具体的文件内容，因此oci-ref只有原生镜像的5%-10%左右。
2. 当容器运行时，向内核态请求具体文件内容时，会通过内核的erofs+fscache特性将请求发送到用户态的apulld进程
3. apulld接收到请求数据后向容器镜像仓库拉取数据，并返回给容器运行时。

通过上述流程形成读取数据的闭环。

# apull部署架构图

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T2036Z/a226f98fa9c64328910e28dcbe91e6b4/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

apulld是一个单独进程，运行在每个容器节点上，由apull-snapshotter启动和管理。主要处理模块：

1. blob manager实现apull snapshotter和apulld之间容器信息交互
2. fscache event handler实现处理fscache事件，例如open/read/close等事件
3. oci-ref offset conversion实现内核文件偏移和OCIv1索引关系转换。由于内核上来的数据是基于erofs文件系统的偏移量，因此需要数据转换成对应的OCIv1的文件
4. fetch image data实现根据上一步转换后的偏移量拉取容器镜像服务器中的数据

# apulld整体流程

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1941Z/6b699577b21048298444519b1b8182c7/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

**控制流**

- 拉取原生镜像的booststrap镜像
- containerd通过grpc调用apull snapshotter
- snapshotter准备镜像rootfs文件系统
- 将镜像信息通过http传送给apulld

**数据流**

- 读取运行时文件数据
- erofs文件系统到后端fscache

cache miss:

- 通过event事件回调到apulld，请求原生容器镜像数据
- 向容器镜像服务器拉取数据

cache hit:

- 从存储介质中读取

# apulld模块介绍

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T2038Z/1eea61856c2445ff818496ad5c573875/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

apulld主要分为4个模块：公共组件、通信模块、业务处理模块和系统处理层。

- 系统处理层主要初始化和系统交互的模块和配置信息。
- 通信模块主要负责对外通信，包括从容器镜像服务器拉取数据和作为HTTP Server从snapshotter获取容器镜像相关配置及其他控制
- 业务处理模块涉及文件缓存管理、容器镜像配置管理、数据转换

## fscache & fscache event handler处理流程

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1943Z/7ea714ded1834bf6b12d0adf75cc98c2/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

分为三个步骤：

1. apull-snapshotter中打开或者从systemd中获取/dev/cachefiles文件句柄，并传递给apulld进程
2. apulld进程对cachefiles初始化并配置cachefiles缓存文件位置或者restore
3. fscache事件接收和处理

下面主要介绍fscache open/read/close事件介绍和处理

### fscache open

当opcode为open操作，data数据结构体如下

```go
type msgHeader struct {
	// msgID indicates the ID of current msg
	msgID uint32
	// opcode used to identify open/read/close operations
	opcode uint32
	// len indicates the msg size
	len uint32
	//objectID indicates the ID of the cache file object, which is unique
	objectID uint32
}

type openMsg struct {
	volume string
	cookie string
	fd     int
	flag   uint32
}
```

读事件上来后，包含几个重要的信息：

- `fsid`,该值包含在cookie中，需要通过解析cookie
- `fd`,该值是cachefile后台匿名句柄，通过这个`fd`写入数据到fscache缓存中

### fscache read

当opcode为read时，进行读相关操作

```go
type msgHeader struct {
	// msgID indicates the ID of current msg
	msgID uint32
	// opcode used to identify open/read/close operations
	opcode uint32
	// len indicates the msg size
	len uint32
	//objectID indicates the ID of the cache file object, which is unique
	objectID uint32
}

type readMsg struct {
	off    uint64
	length uint64
}
```

该事件上来后会根据`objectID`找到open时相同的`objectID`，并找到对应的device驱动设备，执行相关操作，涉及的device驱动设备有两种类型：

1. bootstrap驱动文件
2. blob驱动文件

这两种文件对应文件系统的元数据和原生镜像的blob信息。当读事件需要读取文件系统元数据时会将bootstrap文件写入到`objectID`对应`fd`中。读取blob驱动文件时会从后端读取数据并回写fscache中。

### fscache close

```go
type msgHeader struct {
	// msgID indicates the ID of current msg
	msgID uint32
	// opcode used to identify open/read/close operations
	opcode uint32
	// len indicates the msg size
	len uint32
	//objectID indicates the ID of the cache file object, which is unique
	objectID uint32
}
```

执行关闭对应`objectID`的数据

## container blob manager

container blob manager为了管理每个对应容器镜像的信息。每个容器镜像的配置信息由snapshotter通过HTTP方式传送给apulld。

### 配置信息

```json
{
    "id": "17b578584afd6f1a6efdd0ac81d4ca6200d3e04a99b61de1afa18c118fb6a703",
    "work_dir": "/var/lib/containerd-apull/snapshots/${snapshoid}/fs",
    "backend_config": {
        "host": "rnd-dockerhub.huawei.com:88",
        "repo": "apull/openeuler-23.03",
        "scheme": "https",
    }
}
```

### HTTP接口

apulld容器镜像信息的加载、删除等通过snapshotter http协议传输。具体实现接口如下

| 接口名称         | 接口说明           |
| ---------------- | ------------------ |
| AddBlobConfig    | add blob config    |
| DeleteBlobConfig | delete blob config |

## oci-ref offset conversion

该模块主要处理内核上来的erofs文件系统偏移量和容器OCIv1镜像之间的转换，由于内核上来的偏移量不是容器镜像本身的偏移量，因此无法直接从容器仓库中去下载，需要经过转换。

### oci-ref偏移转换原理

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T2039Z/103663be28b24c7c911d29c7a5b46cbd/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

- 当fscache offset/size请求到达apulld后，apulld过解析oci-ref镜像，比较fscache offset/size与uncompress offset/size范围，得到compress offset/size
- 向容器镜像服务器拉取compress offset/size 的数据
- 解压获取到的compress offset/size 的数据并向内核写入该部分数据

通过上述流程实现内核态offset/size转换到容器镜像offset/size，并获取/解压数据，最终写入到fscache中。

## filecache manager

1. 全量预取
2. 智能预取
3. 超节点内部缓存数据同步

### 全量预取

#### 背景

当使用lazy loading方式启动容器镜像后，每次cache miss时需要容器运行时调用vfs -> erofs -> fscache -> apulld -> register。 整个调用链存在3个问题：

1. 整个调用链非常长，
2. fscache到apulld涉及内核态到用户态的切换
3. apulld到register通过网络，存在网络时延或者网络异常等情况

综上，需要引入一个机制当容器镜像运行过程中减少调用链，减少因为状态切换和网络时延等带来的时延和抖动，提高整体系统的稳定性

#### 方案介绍

容器运行过程中，不是每时每刻都会产生cache miss请求，容器需要的启动数据已经通过启动时的cache miss拉取完毕，剩下镜像数据并非启动必要的数据，这些数据预计占整个容器镜像的90%（参考nginx启动镜像数据）。因此可以通过在非忙时后台自动拉取剩下镜像数据的方式，提前加载到fscache中，实现减少调用链，减少状态切换和网络抖动带来的时延问题。

同时为了减少预取对cache miss等实时性高的请求的干扰，预取应该在相对空闲时进行。现阶段内部全部使用动态线程池控制整个流程，因此可以通过判断当前waitting状态的数量来判断当前是否空闲。**现在代码实现是判断waitting线程数大于总线程数的一半则进行预取**。

#### 实现细节

- 启动参数, config.toml中增加如下配置

```toml
prefetch_enable = true
```

- 流程图
  
  ![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1945Z/7a90452895064cd59af819d9b40a7a3a/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

1. 容器运行时数据请求通过vfs到内核态的erofs文件系统，随后到fscache的cachefiles模块，因为数据不存到而触发cache miss
2. apulld在处理事件请求时注册预取函数到prefetch manager
3. prefetch manager请求线程资源来处理预取
4. 线程池会基于线程资源确认是否可以进行预取，判断的条件是：如果有一半的线程处于waitting状态，则进行预取
5. 预取线程回调到prefether并进行预取操作和写入fscache操作

- 异常情况

1. 预取中间状态不会落盘，因此可能存在：apulld异常退出重启后下一次cache miss会引发再次的全量预取。影响：场景触发概率低，即使存在不会影响性能
2. 预取状态只有两种，1）无拉取 2）已经拉取，没有中间状态的正在拉取中，因此可能会引起重复拉取。影响：无影响

### 超节点内部数据同步

- 超节点部署架构
  ![](https://resource.idp.huawei.com/idpresource/nasshare/editor/image/205652859717/1_zh-cn_other_0000002051793685.jpeg)
  部署apull按需加载组件，启动apull服务和apull-registry服务；

* apull-registry作为镜像服务器，给apull提供镜像数据，接收来自apull的镜像数据请求。
* apull运行容器，发生镜像数据cache miss时，往apull-registry请求数据，如果apull-registry没有该镜像数据，向远端镜像仓库获取数据返回给apull，若apull-registry有该数据则直接返回。
* 当apull-registry出现异常无法提供服务时，apull会切换到向镜像仓库直接请求镜像。

#### 详细设计

![](https://resource.idp.huawei.com/idpresource/nasshare/editor/image/205652859717/1_zh-cn_image_0000002051677229.png)
初始化

* 通过systemd管理apull-registry，并使用普通用户权限运行apull-registry
* apull-registry加载apull-registry.toml配置文件
* apulld启动时配置apull-registry的服务地址

容器运行

* 懒加载方式运行容器时apull接收到cache miss，优先向apull-registry请求镜像层数据
* apull-registry会进行查询是否存在该镜像层数据，如果不存在则向镜像仓库数据请求镜像层数据；如果存在则将数据返回给对端
* 镜像仓库返回数据，apull-registry会保存该镜像数据并返回给对端

### 智能预取

> 设计中

## container image fetch

该模块实现从容器镜像仓库拉取数据。

### registry协议

通常情况下，containerd从镜像层获取镜像数据的步骤为：

1. 从仓库中获取镜像的元数据manifest，manifest记录了镜像的组成等信息；
2. 解析manifest中记录的镜像层数据（layers层）；
3. 依序拉取layers层。layers是真正的镜像数据；
   
   ![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1947Z/eb9bc36ba6e949959e944b60ad24cff8/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

apull方案通过分段拉取方式解决layers层过大导致的镜像下载缓慢的问题。对于较大layers层，apulld根据RegionRequest按需拉取数据。也即，在上述第三步拉取layer数据层时，不再一次性全部拉取，而是在需要某段数据时，将所需信息发送给fetcher模块，由fetcher模块拉取。如下是fetcher拉取某一段镜像layers层的过程。

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1948Z/b480b0acc2a848dc9f1757931e48aa77/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

1. fetcher接受到客户端传递的`RegionRequest`请求，解析成相应的URL和http参数
2. fetcher向authorizer模块获取已有的token信息。
3. fetcher将携带有token信息的请求发送给镜像仓库，尝试访问该网站。若无法访问该网站，则更换scheme参数重新访问。若仍无法访问该网站，则向客户端返回错误，结束流程。
4. fetcher获取镜像仓库返回的请求，并根据其状态决定后续工作：
   - 未授权：可能token失效，通知authorizer刷新token，重试第2步。
   - 超时：重试第2步。
   - 其余状态：向客户端返回消息，结束流程。
     其中，第4步不能重复超过五次，以防止占用过多资源。

### 镜像凭证方案

对于需要凭证（例如：账号密码）的镜像，apulld支持通过读取文件方式获取访问镜像仓库的账号密码。具体地说，apulld通过inotify机制监听凭证文件，若凭证文件存在写系统调用，则内核通知apulld更新重新读取该文件。apulld将凭证信息以map形式存储在内存中，apulld进程退出时信息丢失，启动时重新获取集群中最新凭证数据。

当内核向apulld发送镜像文件请求时，apulld首先会根据镜像仓库地址搜寻对应的凭证列表，并依次使用账号密码（若为密文存储，则先行解密）去获取可用token，或者全部账号均失效返回错误。若某一凭证成功，则apulld将保存该凭证，以便后续优先使用。

#### **凭证格式**

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

#### **使用方式**

- 凭证文件
  
  用户通过apulld配置文件指定凭证文件路径，即
  
  ```toml
  # /etc/apull/config.toml
  [apulld.registry.auths.file]
  	auth_path = "/usr/example.json"
  ```
  
  - 若用户未指定凭证文件，则默认凭证文件路径为`/etc/apull/credentials.json`。apull将会在`/etc/apull`目录下创建空文件`credentials.json`。
  - 在apulld生命周期内，禁止更改凭证文件所在目录的inode（例如删除目录后重建等），否则apulld无法获取最新凭证数据。
  - 在apull启动前，指定凭证文件所在的目录必须存在，否则apulld无法正确监听配置文件变化。
- 密文/明文
  
  用户可通过环境变量`EnableSecretEncrypt`和`PAAS_CRYPTO_PATH`控制apulld以密文或者明文形式使用凭证。
  
  ```bash
  # /usr/lib/systemd/system/apull-snapshotter.service
  Environment="EnableSecretEncrypt=true"
  Environment="PAAS_CRYPTO_PATH=/opt/cloud/cce/srv/kubernetes"
  ```
  
  - `EnableSecretEncrypt`表示凭证是否加密存储。"true"表示凭证为密文，需要解密使用。
  - `PAAS_CRYPTO_PATH`表示密钥根目录，默认为当前目录。密钥由用户提供，需与用户加密密钥目录相同。

# SELinux

## 工作原理

安全增强型 Linux（SELinux）是一种采用安全架构的 Linux系统，它能够让管理员更好地管控用户访问系统。
SELinux 定义了每个用户对系统上的应用、进程和文件的访问控制。当应用或进程（称为主体）发出访问对象（如文件）的请求时，SELinux 会检查访问向量缓存（AVC），其中缓存有主体和对象的访问权限。SeLinux在确认其是否匹配 SELinux 策略数据库的安全环境后，便根据检查授予权限或拒绝。

## apull SELinux安全上下文

在开启SELinux安全策略后，apulld原有进程安全上下文缺少合适的权限挂载erofs文件系统和监听/dev/cachefiles，因此我们提供了apull_t安全上下文标签，为apulld进程和apull-snapshotter进程提供合适的selinux安全上下文。

安装apull rpm包后，可通过如下命令查看`/usr/bin/apulld`和`/usr/bin/apull-containerd-grpc`二进制文件的selinux安全上下文。

```bash
[root@localhost ~]# ls -Z /usr/bin/apulld
system_u:object_r:apull_exec_t:s0 /usr/bin/apulld
[root@localhost ~]# ls -Z /usr/bin/apull-containerd-grpc 
system_u:object_r:apull_exec_t:s0 /usr/bin/apull-containerd-grpc
```

启动`apull-snapshotter`服务后（`systemctl start apull-snapshotter.service`），可通过如下命令查看进程安全上下文标签。

```bash
[root@localhost ~]# ps -efZ | grep apull
system_u:system_r:apull_t:s0    root      365873       1  0 20:42 ?        00:00:00 /usr/bin/apull-containerd-grpc
system_u:system_r:apull_t:s0    root      365879  365873  0 20:42 ?        00:00:00 /usr/bin/apulld --socket-address /var/run/apulld.sock --cache-file 7 --log-level info --log-driver stdout
```

在开启SELinux策略后，apulld正常运行。

```bash
[root@localhost ~]# setenforce 1
[root@localhost ~]# sestatus | grep Current
Current mode:                   enforcing
[root@localhost ~]# systemctl restart apull-snapshotter
[root@localhost ~]# systemctl status apull-snapshotter
● apull-snapshotter.service - apull snapshotter
     Loaded: loaded (/usr/lib/systemd/system/apull-snapshotter.service; disabled; vendor preset: disabled)
     Active: active (running) since Wed 2024-05-29 10:58:37 CST; 5s ago
    Process: 879477 ExecStartPre=/sbin/modprobe cachefiles (code=exited, status=0/SUCCESS)
    Process: 879481 ExecStartPre=/sbin/modprobe erofs (code=exited, status=0/SUCCESS)
    Process: 879485 ExecStartPre=/bin/bash -c if [[ "$(cat /sys/module/fs_ctl/parameters/erofs_enabled)" !=  "Y" ]];then                                  echo "erofs>
    Process: 879489 ExecStartPre=/bin/bash -c if [[ "$(cat /sys/module/fs_ctl/parameters/cachefiles_ondemand_enabled)" !=  "Y" ]];then                               >
   Main PID: 879495 (apull-container)
      Tasks: 18 (limit: 47299)
     Memory: 17.4M
     CGroup: /system.slice/apull-snapshotter.service
             ├─ 879495 /usr/bin/apull-containerd-grpc
             └─ 879501 /usr/bin/apulld --socket-address /var/run/apulld.sock --cache-file 7 --log-level info --log-driver stdout
```

目前规则尚未完善，例如`apull_t`只支持`apulld.cache_dir`参数为`/var/lib`下目录的场景，对于其它目录，可能会在`/var/log/audit/audit.log`日志文件中出现avc 拒绝的日志。但由于apull_t规则中设置了`permissive apull_t`，因此SELinux允许apulld继续执行该操作但会通过audit日志告警。

可通过如下方式为apull_t增加权限：

1. 观察audit日志获取apulld权限被拒绝的日志，并保存在文件avc.log中：

```
type=AVC msg=audit(1716795060.946:197): avc:  denied  { write } for  pid=1581 comm="apull-container" name="snapshots" dev="dm-0" ino=3014910 scontext=system_u:system_r:apull_t:s0 tcontext=system_u:object_r:var_lib_t:s0 tclass=dir permissive=1
```

2. 通过avc日志生成所需selinux规则：
   
   ```
   [root@localhost apull]# cat avc.log  | audit2allow -R
   require {
           type apull_t;
   }
   
   #============= apull_t ==============
   files_rw_var_lib_dirs(apull_t)
   ```
3. 将规则加入apull.te文件并重新生成相应规则，可执行`make selinux`。

# 镜像格式

# 通信矩阵

1. apulld和apull-snapshot进程
   
   | 客户端             | 服务端     | 通信端口            | 通信协议    |
| ------------------ | ---------- | ------------------- | ----------- |
| apull-snapshot进程 | apulld进程 | /var/run/apull.sock | unix-socket |
   
   
2. apulld与远程镜像仓库通信，获取apulld所需的镜像层数据
   
   | 源设备           | 源IP           | 源端口      | 目的设备           | 目的IP           | 目的端口      | 端口名称                               | 协议   | 加密方式 | 认证方式 |
| ---------------- | -------------- | ----------- | ------------------ | ---------------- | ------------- | -------------------------------------- | ------ | -------- | -------- |
| apulld服务端设备 | apulld服务端IP | 32768~60999 | 镜像仓库服务端设备 | 镜像服务端IP | 80/443（可配置） | apulld作为客户端访问镜像服务的连接端口 | tcp    | tls      | token    |
   
   

# 通信协议

1.apull-snapshot与apulld进行http通信，完成配置加载与删除，以及apulld进程保活检查

| 通信协议 | Method | URL                       | Content-Type | 发送数据                        | 返回数据                               |
| -------- | ------ | ------------------------- | ------------ | ------------------------------- | -------------------------------------- |
| HTTP     | PUT    | /v1.0/apulld/blob_config  | json         | blob config(容器镜像的配置信息) | statusOK/(statusBadRequest + errormsg) |
| HTTP     | DELETE | /v1.0/apulld//blob_config | json         | blob_id                         | statusOK/(statusBadRequest + errormsg) |
| HTTP     | PUT    | /v1.0/apulld/ping         | json         | NA                              | statusOK                               |

# 配置信息

该模块主要说明apulld的哪个模块需要保存什么配置，方便其他接口获取时读取相关配置以及方便评审配置是否合理。

```toml
[global]
log_level = "info"

[apulld]
# fscache directory
cache_dir = "/var/lib/contained-apull/cachefiles"
# thread_num sets the nums of thread pool
thread_num = 4

# credential file path
[apulld.registry.auths.file]
  auth_path = "/etc/apull/credentials.json"
```

# 异常场景说明

1. apulld异常退出
   
   - 可自行恢复
2. systemd stop apull-snapshotter后重新拉起，容器无法访问到未缓存到cachefiles中的数据，并报错
   
   - 原因：/dev/cachefiles句柄生命周期和apull-snapshotter.service生命周期一致，即在apull-snapshotter/apulld/restart service等操作不会导致句柄被释放，只有在stop service时句柄会被释放。句柄释放会导致已经挂载的erofs后端文件系统的fscache请求不会到新打开的/dev/cachefiles句柄中，导致已经挂载的文件系统无法读取文件
   - 结果：导致容器运行失败
   - 缓解方法：apulld会预取提前缓存，如果缓存已经在cachefiles中则不会报错
   - 恢复方法：重新挂载容器erofs文件系统
3. cachefiles后端存储满，导致apulld无法写入，容器进程读取失败
   
   - 原因：cachefile后端存储满导致apulld处理完fscache请求后无法将文件内容写入fscache，此时该请求对应的容器运行时进程会收到读文件报错。此时需要进程做好相关异常处理
   - 结果：导致容器无法读取文件而报错退出，apulld写入cachefiles异常报错
   - 缓解方法：内核态正在考虑直通方案，即当cachefiles后端满时不写入cachefiles后端存储，而是直接返回给容器运行时进程。但是这种做法会使容器运行时进程运行效率降低，但不会导致容器运行时进程异常。（暂不支持）
   - 恢复方法：删除不需要运行的容器镜像或者重新设置cachefiles后端存储路径

# 约束条件

1. 容器镜像索引生成只支持tar.gz压缩的容器镜像包
