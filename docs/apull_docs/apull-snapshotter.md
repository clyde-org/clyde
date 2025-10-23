# Introduction

apull是强大高效的容器镜像快速加载解决方案。它基于内核erofs文件系统和fscache实现bootstrap(空文件系统)加载，以及在用户态apulld中处理内核blob数据(文件数据)请求实现容器镜像懒加载和快速启动。该方案提升了容器启动速度、提升网络带宽效率、减少镜像空间占用以及校验数据完整性，为云原生工作负载容器镜像等提供高效快速的分发和启动能力。

该特性主要涉及用户态两个模块：

- apull-snapshotter: 支持erofs文件系统挂载的containerd snapshotter插件
- apulld: 处理fscache事件请求，并从容器镜像服务器拉取容器镜像数据

本文档主要介绍apull-snapshotter设计架构

# apull-snapshotter

apull-snapshotter主要实现：

1. 实现containerd的grpc调用并根据调用流程完成erofs文件系统的挂载
2. 启动、配置、管理apulld进程

对比原生snapshotter主要差异在于：

1. apull-snapshotter支持lazy loading的erofs bootstrap文件系统挂载
2. apull-snapshotter启动、配置、管理apulld进程

# apull软件部署架构图

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T2035Z/d8ee1d33a9384e9180b803a018d5ccd1/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

图中apull-snapshotter作为containerd的插件，是一个单独进程，运行在每个容器节点上，由systemd拉起和管理，主要功能：

- 实现containerd的gRPC调用
- 启动、配置、管理apulld等操作

# apull-snapshotter整体流程

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1922Z/45e1adb1440f400ca7a56f55465cfefd/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

**镜像拉取**

处理每层镜像

- 该层为bootstrap层
  - 准备只读层snapshots
  - containerd拉取该bootstrap层
- 该层为blob层
  - 准备只读层snapshots
  - 将该snapshots commit
  - 返回ErrAlreadyExists给containerd，不拉取该层

**容器运行**

- 准备读写层snapshots
- 挂载erofs镜像
- 将镜像信息通过http传送给apulld

**容器删除**

- 删除读写层snapshots

**镜像删除**

删除每层镜像

- 该层为bootstrap层
  - 卸载erofs镜像
  - 发送fsid给apulld，删除bootstrap cache
  - 删除bootstrap层对应的snapshots
- 该层为blob层
  - 发送fsid和blobID给apulld，删除blob cache
  - 删除blob层对应的snapshots

判断bootstrap和blob：

- 从每层镜像的Annotation中获取标志

# 软件架构UML

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1924Z/b74f4d13c98448e8969844d02a23092a/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

## 一 apull-snapshotter接口

*SnapshotterService* 接口注册到grpc调用框架后会被containerd通过grpc.sock调用到，具体由*snapshotter*类实现，其中主要实现如下接口

| 接口名称 | 接口说明                                                                                                                                                                                                          |     |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --- |
| State    | Stat returns the info for an active or committed snapshot by name or key                                                                                                                                          |     |
| Update   | Update updates the info for a snapshot.                                                                                                                                                                           |     |
| Usage    | Usage returns the resource usage of an active or committed snapshot excluding the usage of parent snapshots.                                                                                                      |     |
| Mounts   | Mounts returns the mounts for the active snapshot transaction identified by key. Can be called on a read-write or readonly transaction. This is available only for active snapshots.                              |     |
| Prepare  | Prepare creates an active snapshot identified by key descending from the provided parent.  The returned mounts can be used to mount the snapshot to capture changes.                                              |     |
| View     | View behaves identically to Prepare except the result may not be committed back to the snapshot snapshotter. View returns a readonly view on the parent, with the active snapshot being tracked by the given key. |     |
| Commit   | Commit captures the changes between key and its parent into a snapshot identified by name.  The name can then be used with the snapshotter's other methods to create subsequent snapshots.                        |     |
| Remove   | Remove the committed or active snapshot by the provided key.                                                                                                                                                      |     |
| Walk     | Walk will call the provided function for each snapshot in the                                                                                                                                                     |     |
| Close    | Close releases the internal resources.                                                                                                                                                                            |     |
上述接口中 *State*、*Update*、*Usage* 、*Commit* 、*Remove*、*Walk*、*Close* 和原生snapshotter实现相同，无需再次开发，重点详细介绍 *Mounts*、*Prepare*、*View* 三个接口

*Mounts*、*Prepare*、*View* 这三个函数入参如下

```go
func (o *snapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error)
func (o *snapshotter) Prepare(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error)
func (o *snapshotter) View(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error)
```

- context.Context 参数传递containerd的相关参数，主要用于公共库的初始化和操作，相关实现都是在开源公共库中实现，只需要仿照使用即可。
- key, parent是该层的layer id以及父层layer id。
- snapshots.Opt是一个回调函数，主要用于获取snapshots层的信息，如下：

```go
// Info provides information about a particular snapshot.
// JSON marshalling is supported for interacting with tools like ctr,
type Info struct {
	Kind   Kind   // active or committed snapshot
	Name   string // name or key of snapshot
	Parent string `json:",omitempty"` // name of parent snapshot

	// Labels for a snapshot.
	//
	// Note: only labels prefixed with `containerd.io/snapshot/` will be inherited
	// by the snapshotter's `Prepare`, `View`, or `Commit` calls.
	Labels  map[string]string `json:",omitempty"`
	Created time.Time         `json:",omitempty"` // Created time
	Updated time.Time         `json:",omitempty"` // Last update time
}
```

通过上述可以知道，通过调用snapshots.Opt可以获得当前层的相关信息。

### *Prepare* 接口设计

prepare函数准备创建从提供的父级到子级标致的snapshotter层，并返回mount的参数。不执行mount操作。

```go
输入：ctx context.Context, key, parent string, opts ...Opt
输出：[]mount.Mount, error
```

其中`key`是当前snapshot 引用层，是当前镜像层sha256值和随机数拼接的唯一字符串，parent是上一层镜像层的sha256，这两个值用来标记`snapshotter`数据库中当前镜像层的唯一值以及和上一层镜像层的联系，由`containerd`自身函数实现。opts是传入的属性函数，存储了当前镜像层的详细信息。

首先`createSnapshot`，主要是调用原生的CreateSnapshot和创建该`snapshot`层工作目录，根据传入的opt函数，得到`snapshot`的信息`info`

```go
type Info struct {
    Kind   Kind   
    Name   string
    Parent string `json:",omitempty"` 
    Labels  map[string]string `json:",omitempty"`
    Created time.Time         `json:",omitempty"` 
    Updated time.Time         `json:",omitempty"`
}
```

判断`info.labels`中是否含有键值`label.TargetSnapshotRef`

如果存在键值`label.TargetSnapshotRef`，则该接口是拉取镜像时调用。拉取每层镜像时会调用该接口准备`commited`的`snapshot`层。通过查看`info.labels`是否存在`ApulldMetaLayer`和`ApullDataLayer`键值，分别处理对应的`bootstrap`层和`blob`层。

对`blob`层来说，不需要做任何操作，只需要将该层提交为一个`commited`状态，返回错误`errdefs.ErrAlreadyExists`，由`containerd`识别，跳过该层的操作。

对`bootstrap`层来说，调用`mount`接口，返回`mounts`数组(包含`lowerdir`，`upperdir`，`workdir`等目录信息)和nil，`containerd`会继续调用相应的函数，解压该层信息至`upperdir`层。

如果不存在键值`label.TargetSnapshotRef`，则该接口是创建容器时调用，创建一个`active`的`snapshot`层。这时会去调用`filesystem`的`mount`接口，将`lowerdir`层挂载至`erofs`系统，接着调用`remoteMounts`，返回`overlayfs mounts`数组。

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1927Z/58f53d5a91d94529bcfa2eb5a1ebd209/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

### *Mounts* 接口设计

该接口主要返回mount的相关信息，通过ID查询当前层lowerdir、upperdir等信息，并实现挂载镜像操作

```go
输入：ctx context.Context, key, parent string, opts ...Opt
输出：[]mount.Mount, error
```

需要判断key值的父层是否为`bootstrap`层，如果是的话，需要调用`remoteMounts`，如果不是，则调用`mounts`。`remoteMounts`函数返回的`overlayfs mounts`数组中的`lowerdir`为挂载到`erofs`文件系统的目录，函数`mounts`返回的`overlayfs mounts`数组中的`lowerdir`为所有的只读层。

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1928Z/577470540b804045ac8da92507999f00/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

### *View* 接口设计

view的操作和prepare相同，但是view返回的mounts的数组是只读层(即lowerdir)。

```go
输入：ctx context.Context, key, parent string, opts ...Opt
输出：[]mount.Mount, error
```

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1929Z/75bd23fd41224358b48e9620b7cf9760/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

### *Remove* 接口设计

remove接口的目的是删除镜像层数据。删除镜像时，`containerd`由上到下逐层删除镜像，即每删一层调用一次`Remove`接口。对于每一层的删除，如果是懒加载的`bootstrap`层，首先卸载`erofs`挂载点，然后删除对应的`cachefile`，如果是懒加载的`blob`镜像层，删除对应的`cachefile`。

![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1929Z/11d0cd8700cf41c5805b1803816da0e6/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

## 二 Filesystem

该模块负责挂载只读层至erofs文件系统，包含以下功能：

1. 拉起`apulld`进程，实时检测`apulld`状态；
2. 实现和apulld通信，起容器时将镜像相关配置信息发送至`apulld`，删除镜像时发消息给`apulld`删除对应的配置信息；
3. 只读层挂载和卸载。
   
   ![image](https://wiki.huawei.com/vision-file-storage/api/file/download/upload-v2/2024/4/27/20240527T1929Z/f59631c448b2428d9712fdfe1df34042/image.png?appKey=56f69231-0ee9-11ed-8d72-fa163ecf9d11)

### Newfilesystem

拉起`apulld`进程，实时检测`apulld`状态。

### Mount

将只读层挂载，为只读层(元数据层)挂载至`erofs`文件系统准备所有必要的资源`blob_config.json`，并将该配置信息通过`unix socket`通信，传送给`apulld server`。

### Umount

将只读层卸载，并通知`apulld server`删除相关配置信息。

# 通信矩阵

|        客户端         |        服务端         |                     通信端口                     |  通信协议   |
| :-------------------: | :-------------------: | :----------------------------------------------: | :---------: |
|    containerd进程     | apull-snapshotter进程 | /var/run/containerd-apull-grpc.sock | unix-socket |
| apull-snapshotter进程 |      apulld进程       |               /var/run/apulld.sock               | unix-socket |

# 数据落盘

1. metadata.db，和containerd保持一致，存储着snapshots`Info`信息
2. config.json，每一个镜像的详细说明`IndexImageInfo`，包含挂载点、发送给apulld的镜像信息`Bootstrap`

```json
{
  "sapshot_id": "4",
  "fscache_id": "29d618ad5410d7ed388512832346f11d7751eb8b4a8bcb02a9f9e64ba3db9da3",
  "image_id": "rnd-dockerhub.huawei.com:88/apull/openeuler-linux-src-apull:latest",
  "snapshot_dir": "/var/lib/containerd-apull/snapshots/4",
  "config_dir": "/var/lib/containerd-apull/config/4",
  "mount_point": "/var/lib/containerd-apull/snapshots/4/mnt",
  "bootstrap": {
    "id": "29d618ad5410d7ed388512832346f11d7751eb8b4a8bcb02a9f9e64ba3db9da3",
    "backend_config": {
      "host": "rnd-dockerhub.huawei.com:88",
      "repo": "apull/openeuler-linux-src-apull",
      "scheme": "https"
    },
    "work_dir": "/var/lib/containerd-apull/snapshots/4/fs"
  }
}
```

# 文件权限

| 文件/目录                                                    | 用途                                      | 权限说明 | 备注             |
| :----------------------------------------------------------- | :---------------------------------------- | :------- | ---------------- |
| /usr/bin/apull-containerd-grpc                       |      apull-containerd-grpc二进制文件         | 550      |    |
| /usr/bin/apulld                        | apulld二进制文件              | 550      |    |
| /var/lib/containerd-apull/                        | apull数据目录，包含运行数据、缓存数据、镜像数据等              | 700      |    |
| /var/lib/containerd-apull/metadata.db                        |snapshots信息数据库                 | 600      |    |
| /var/lib/containerd-apull/cachefiles                       | fscache后端存储根目录                  | 700      |    |
| /var/lib/containerd-apull/cachefiles/cache                        | fscache后端存储目录                  | 700      |    |
| /var/lib/containerd-apull/cachefiles/graveyard                        | fscache后端存储目录                  | 700      |    |
| /var/lib/containerd-apull/snapshots  |镜像数据总目录                      | 700      |    |
| /var/lib/containerd-apull/snapshots/snapshotID  | 每一层镜像层的数据目录                      | 700      |    |
| /var/lib/containerd-apull/snapshots/snapshotID/fs  | 每一层镜像层的fs目录                      | 750      |    |
| /var/lib/containerd-apull/snapshots/snapshotID/work | 每一层镜像层的work目录                    | 700      |    |
| /var/lib/containerd-apull/snapshots/snapshotID/fs/image.boot  | 索引镜像的meta文件                      | 440      |    |
| /var/lib/containerd-apull/snapshots/snapshotID/fs/blobMetaID  | 每一层镜像层的meta文件                      | 440     |    |
| /var/lib/containerd-apull/snapshots/snapshotID/mnt           | 每一个镜像实例的挂载点                    | 755      | 容器镜像实例权限 |
| /var/lib/containerd-apull/config                             | 镜像实例config根目录                            | 750      |                  |
| /var/lib/containerd-apull/config/snapshotID                  | 每一个镜像实例的config目录                | 750      |                  |
| /var/lib/containerd-apull/config/snapshotID/config.json      | 每一个镜像实例的详细信息                  | 600      |                  |
| /usr/lib/systemd/system/apull-snapshotter.target          | systemd配置文件，用于管理apull-snapshotter服务 | 640      |  |
| /usr/lib/systemd/system/apull-snapshotter.service           | systemd配置文件，用于管理apull-snapshotter服务 | 640      |  |
| /var/run/containerd-apull                                        | containerd和apull-snapshotter通信地址目录 | 700      |                  |
| /var/run/containerd-apull-grpc.sock             | containerd和apull-snapshotter通信地址     | 600      |                  |
| /var/run/apull-containerd-grpc.pid          | 存放apull-containerd-grpc的PID | 600      |  |
| /var/run/apulld.pid          | 存放apulld的PID | 600      |  |
| /etc/apull/           | apulld配置文件根目录 | 750      |  |
| /etc/apull/config.toml           | apulld配置文件，包含设置apulld日志级别，后端存储目录，线程数等 | 640      |  |
| /etc/apull/credentials.json           | 镜像仓库凭证文件 | 600      |  |

# 软件依赖

- Kernel 支持erofs over fscache
- containerd >= 1.6.14

# containerd配置修改支持apull-snapshotter

1. 修改containerd配置文件`/etc/containerd/config.toml`

```toml
[proxy_plugins]
  [proxy_plugins.apull]
    type = "snapshot"
    address = "/var/run/containerd-apull-grpc.sock"
```

启动过程中指定containerd使用的snapshotter，如下：

```shell
nerdctl --snapshotter apull run --rm --insecure-registry rnd-dockerhub.huawei.com:88/apull/openeuler-23.03:linux-src-ref
```

# 异常场景说明

1. apull-snapshotter异常退出
   
   - 可自行恢复
2. 原生snapshotter和apull-snapshotter切换，一共存在如下场景：
   
   1. 原生镜像由原生snapshotter下载，再切换到apull-snapshotter
      
      - 正常运行
   2. 原生镜像由apull-snapshotter下载，再切换到原生snapshotter
      
      - 正常运行
   3. 索引镜像由原生snapshotter下载，再切换到apull-snapshotter
      
      - 运行失败
   4. 索引镜像由apull-snapshotter下载，再切换到原生snapshotter
      
      - 运行失败

# 约束条件

1. 容器存在时，禁止用户手动卸载该容器镜像的erofs挂载点，否则会导致该容器镜像不可用。

- 原因：卸载erofs挂载点时，容器的overlayfs挂载点仍然存在，导致内核fscache_cookie引用计数无法释放，再次启动该镜像容器时，内核无法重新挂载该erofs挂载点。
- 解决方法：删除使用该容器镜像的全部容器后，即可重新使用该容器镜像。
