# sandwichcake 设计文档

符合 OCI 的镜像**组合**工具，可作为 Go 库调用，也提供 CLI。

重点场景：发行版 + CUDA toolkit + Python venv + 模型权重。

## 1. 定位与哲学

- 组合远端 Container Registry 中已有的 layer 成新的 tag，而非本地从头构建。不提供 `docker build` 类功能。
- 复用已有层；减少重复传输；产物是标准 OCI 镜像，任何运行时可直接 `pull`/`run`。
- 平台：Linux、macOS 为主；Windows 仅作为**客户端**支持，不支持 Windows 容器镜像。
- 语言 Go，核心依赖 `github.com/google/go-containerregistry`。

## 2. 核心概念

### 2.1 层的三种分类

| 类型 | 来源 | 位置约束 | 组合安全性 |
|---|---|---|---|
| **基础层 (base)** | 普通基础镜像（`nvidia/cuda:*-devel`、`ghcr.io/astral-sh/uv` 等）的**全部**层，按原顺序整体引入 | 只能作为第一段 | 安全（保持原镜像内部的 diff 链完整） |
| **文件层 (file)** | sandwichcake 自己打包的目录（venv、权重、二进制、patch、other） | 任意位置 | 安全（自包含、无 whiteout、根路径互不重叠，组合前做路径冲突检测） |
| **外部层 (external)** | 从其他镜像中抽取的单个/若干层 | 任意位置 | 不保证；需通过 history 判定为"纯文件层"，否则必须 `--force` |

**外部层的"纯文件"判定**（按可信度递减）：

1. 该层对应的 `history[].created_by` 匹配 `COPY` / `ADD`（含 buildkit 形式 `COPY ... # buildkit` 与旧版 `/bin/sh -c #(nop) COPY file:... in /...`）→ 视为纯文件层。
2. 该层本身带 `com.github.baba.sandwichcake.layer.type` 注解（即由 sandwichcake 打包，只是被当外部层引用）→ 等同文件层。
3. 其余（`RUN`、无 history、history 与 layers 无法对齐）→ 拒绝，提示用 `--force`，或用 `--verify` 下载该层扫描 tar 头：无 `.wh.` 条目且路径与其他层不重叠才放行。

注意：history 与 layers 的对齐依赖 `empty_layer` 字段，解析时需跳过 `empty_layer: true` 的条目。

### 2.2 Slice：sandwichcake 上传产物的锚点

OCI registry 中 blob 只能通过 manifest 被引用，未被任何 manifest 引用的 blob 会被 GC。因此 `pack` 的产物不能是"裸层"，而是一个 **slice**：

- 一个标准的 OCI image manifest（`application/vnd.oci.image.manifest.v1+json`），config 使用标准 `application/vnd.oci.image.config.v1+json`（最小内容：`architecture`/`os`、`rootfs.type=layers`、`rootfs.diff_ids` 与层一一对应、每层一条 `history`）。不使用自定义 config mediaType，以兼容不支持 OCI artifact 的 registry。
- `architecture`/`os` 是 config 的必填字段。arch 相关 slice 填真实值；arch 无关 slice（权重、配置）填 `os: unknown, architecture: unknown`——这是 BuildKit attestation manifest 已在用的惯例，Docker Hub / GHCR 都接受——并加注解 `com.github.baba.sandwichcake.platform=any`。副作用是 `docker pull` 一个 slice 会因平台不匹配失败，slice 本来就不用于运行，可接受。
- manifest 注解 `com.github.baba.sandwichcake.kind = "slice"`，`layers[].annotations` 携带每层元数据（见 2.3）。
- 一个 slice 推送到一个 tag。`cake.yaml` 中 `ref: repo:tag` 引用一个 slice 即引用其全部层；`ref: repo@sha256:...` 引用 slice manifest 的 digest。
- 组合产物（最终镜像）在 manifest 注解中记录 `com.github.baba.sandwichcake.slices`（所用 slice 的 `repo@digest` 列表）作为溯源；同时 `compose` 执行后生成 `cake.lock`，把所有 tag 解析为 digest，保证可重现。

权重是 slice，venv 是 slice，一个基础镜像的"全部层"在数据模型上也抽象成 slice（kind=base，只是不由我们上传）。`compose` 的输入统一为 slice 列表。

### 2.3 元数据：history + annotations 双轨

- `history[].created_by`：人类可读，形如 `sandwichcake pack --type venv --root /opt/venv ./venv`，兼容 `docker history`。
- `layers[].annotations`：机器可读，命名空间 `com.github.baba.sandwichcake.*`（反向域名，已定，之后不可改）。slice 的层进入最终镜像时注解**原样带入**，`inspect` 最终镜像即可看到每层来历。

注解分两级。**通用注解**由框架写入、与 type 无关，`compose`/`inspect`/`plan` 只依赖这一级：

```
com.github.baba.sandwichcake.layer.type       venv | weight | binary | patch | other | …（type 名，见 2.7）
com.github.baba.sandwichcake.layer.schema     该 type 注解格式的版本号，如 "1"
com.github.baba.sandwichcake.layer.root       层内文件的公共根路径，如 /opt/venv
com.github.baba.sandwichcake.layer.name       逻辑名
com.github.baba.sandwichcake.layer.part       自动分层时的序号 "3/7"
com.github.baba.sandwichcake.layer.files      本层包含的文件数
com.github.baba.sandwichcake.layer.overlay    "true" 表示本层有意覆盖其他层的路径（patch 用），跳过冲突检测
com.github.baba.sandwichcake.layer.requires   对 base 的运行要求，逗号分隔，如 "glibc>=2.17"；框架统一校验
com.github.baba.sandwichcake.source.image     external 层：来源镜像 repo@digest
com.github.baba.sandwichcake.source.index     external 层：在来源镜像中的层下标
com.github.baba.sandwichcake.tool.version     sandwichcake 版本
```

**type 专有注解**位于 `com.github.baba.sandwichcake.<type>.*`，由各 type 自己定义和解释，框架视为不透明字符串；具体内容见 2.7。

**大体积元数据作为 OCI Artifact 另外推送**。Registry 对 manifest 大小有上限（常见 4 MiB），lock 文件全文、权重文件清单（每个文件的路径/大小/sha256）、pip freeze 输出等不进注解、也不进层（避免污染 diff_id）。做法：

- `pack` 完成 slice 推送后，再推送一个 **metadata artifact**（OCI 1.1 image manifest）：
  - `artifactType: application/vnd.sandwichcake.metadata.v1`
  - `config`: `application/vnd.oci.empty.v1+json`（`{}`，digest 固定）
  - `subject`: 指向 slice manifest 的 descriptor
  - `layers[]`: 每个元数据文件一个 blob，mediaType 如 `application/vnd.sandwichcake.uv.lock`、`application/vnd.sandwichcake.files+json`，`org.opencontainers.image.title` 注解给出文件名
- 发现：`GET /v2/<name>/referrers/<slice-digest>`；registry 不支持 referrers API 时按 OCI 1.1 规定回退到 tag `sha256-<hex>`（ggcr `remote.Referrers` 已处理两种情况）。
- 兼容性：个别旧 registry 拒绝 `vnd.oci.empty` config 或 `subject`，提供 `--artifact-compat` 改用 `application/vnd.oci.image.config.v1+json` 作 config 并只走 tag 回退。
- `inspect` 自动跟随 referrers 展示元数据；`rm` 删除 slice 时一并删除其 referrers。
- 层注解里保留 `*.digest`（如 `venv.lock.digest`），即使 artifact 丢失也能校验。

### 2.4 venv 自带 Python 解释器

venv 不自包含是组合里最脆弱的依赖：`pyvenv.cfg` 的 `home` 和 `bin/python` 符号链接都指向 base 里的解释器，换 base 就断。因此 **venv slice 默认自带解释器**（`--python embedded`，可用 `--python system` 退回依赖 base）。

- 解释器来源：[python-build-standalone](https://github.com/astral-sh/python-build-standalone)（PBS，uv 管理 Python 的同一来源）的 `install_only_stripped` 发行版，可重定位，`*-linux-gnu` 只依赖 glibc ≥ 2.17，`*-linux-musl` 用于 Alpine。
- **不能只替换 `venv/bin/python` 这一个文件**：PBS 的 `python` 可执行文件离不开同发行版的 `lib/python3.X/` 标准库（约 30–40 MiB）与 `lib/libpython3.X.so`（shared 变体）。venv 的 `bin/python` 本来就是指向 `pyvenv.cfg: home` 所在解释器的符号链接，解释器通过 `home` 向上找 `lib/python3.X/os.py` 定位 stdlib。因此做法是**把整个 PBS 发行版放进 venv 目录内，让 `home` 指向它**，venv 成为单 root、可整体搬运的目录：
  ```
  /opt/venv/
    .python/                      PBS 发行版（bin/、lib/、share/），独立层
    bin/python  -> ../.python/bin/python3.12
    bin/python3 -> python
    bin/*                         入口脚本
    lib/python3.12/site-packages/
    pyvenv.cfg                    home = /opt/venv/.python/bin
  ```
  - `pack` 把 `<root>/.python/**` 单独成层（不与 site-packages 混装），所以同版本解释器、同 root 的 venv slice 之间该层 diff_id 相同、可复用。root 不同则不共享，这是单 root 换来的代价，对"大家都用 `/opt/venv`"的实际情况影响不大。
  - 不采用两 root 布局（解释器放 `/opt/sandwichcake/python/<ver>`）：能跨 root 共享解释器层，但引入多 root slice、两 slice 引用同一解释器路径时的去重规则等复杂度，收益不对等。
  - 不采用"不建 venv、直接往 PBS 的 site-packages 装包"：虽然最简单，但 `uv sync` 只认 venv，`uv pip install --system` 丢失 lock 语义，且很多工具假设 `VIRTUAL_ENV` 存在。
- 对 base 的要求降为 **libc 版本**：`com.github.baba.sandwichcake.venv.libc` 与 base 校验（base 的 glibc 版本可从 `com.github.baba.sandwichcake.*` 注解、用户声明获得，或 `--verify` 时读取 base 层中 `libc.so.6` 的文件名/`ld-linux` 版本），不再需要 base 带特定 Python。manylinux wheel（如 torch 的 `manylinux_2_28`）对 glibc 的要求同样纳入该字段。
- 路径一致性：venv 内含绝对路径（`pyvenv.cfg` 的 `home`、`bin/*` 脚本的 shebang、`.pth`，以及 PBS 的 `_sysconfigdata_*.py` / `config-*/Makefile` 中的安装路径——uv 安装时会把后者改写为实际路径）。`pack` 要求 venv 在打包机上的路径与 `--root` 一致；不一致时 `--rewrite-prefix <src>=<root>` 对上述文件做文本替换（`uv venv --relocatable` 生成的 shebang 为相对 trampoline，无需处理）。`__pycache__/*.pyc` 中嵌入的源路径只影响 traceback 显示，不处理。
- 便利命令：`swcake pack venv --from-lock uv.lock --python 3.12 --root /opt/venv --to <repo:tag>`：调用本机 uv，`UV_PYTHON_INSTALL_DIR=<tmp>/opt/venv/.python` 装解释器、`uv venv` + `uv sync --locked` 生成 venv，再 `--rewrite-prefix <tmp>=/` 打包；uv 不存在则报错，不内置 uv。
- 架构：自带解释器后 venv slice 一定是 arch 相关的，按 `--platform` 标注；跨架构的 venv 发布为 image index，每个平台一个 manifest。
- PBS 的已知怪癖（libedit 而非 readline、terminfo 路径、不含 tix）对服务端推理场景无影响，文档中提及即可。

### 2.5 层的身份与确定性

- 复用判定以 **diff_id**（解压后 tar 的 sha256）为准，传输判定以 **digest**（压缩后 blob）为准。
- 打包必须**确定性**：条目按路径排序、固定 mtime（默认 `SOURCE_DATE_EPOCH` 或 epoch 0）、忽略 atime/ctime、PAX 头去除非必要字段、符号链接原样保留、不跟随、硬链接按首次出现的路径展开为普通文件。相同目录两次打包必须得到相同 diff_id。
- **tar 内容规则**（这些直接影响叠加到 base 上的结果）：
  - 条目路径不带前导 `/`，与 Docker/BuildKit 产出一致（`opt/venv/bin/python`）。
  - **祖先目录条目**：root 以上的目录（如 `opt/`）在层中出现时，其 mode/uid/gid 会**覆盖** base 中同名目录的属性——这是 OCI 叠加语义，不是 bug。默认与 BuildKit `COPY` 行为一致：为缺失的祖先写入 `0755 root:root` 条目（运行时对缺失父目录的隐式创建权限各家不同，不能依赖）。root 落在 `/home/<user>/...` 之类非 root 所有目录下时会改变属主，`--no-parents` 可完全不写祖先条目，由用户确保 base 已有该路径。
  - **属主与权限**：默认 `uid/gid = 0`，目录 0755、文件保留原 exec 位（0755/0644）。容器以非 root 运行时 venv 不可写（`__pycache__` 写失败只是静默降速，权重只读无影响）；需要可写时 `--owner <uid>:<gid>` / `--chmod`。uid/gid 不随打包机变化，是确定性的一部分。
  - 不产生 whiteout（`.wh.*`）；源目录中若有同名文件直接报错。
  - 目录条目只为 root 及其子目录写入，保证两个 slice 的 root 互不为前缀时 tar 条目集合也不相交。
- 压缩：
  - gzip（默认，兼容性最好，多线程用 `klauspost/pgzip`）
  - zstd（`application/vnd.oci.image.layer.v1.tar+zstd`，多线程用 `klauspost/compress/zstd`，需 Docker ≥ 23 / containerd ≥ 1.5）
  - none（`application/vnd.oci.image.layer.v1.tar`，此时 digest == diff_id）
  - 压缩等级可配置；`weight` type 默认 `none`（safetensors/gguf 基本不可压缩，省 CPU 和时间）。
  - 多线程压缩输出与线程数无关（pgzip/klauspost zstd 分块策略固定）。即便某个版本不满足，复用判定按 diff_id、不按 digest，不影响正确性，只多一次传输；不为此单独加测试。
- `v1.Image` 的 config `created` 时间也取 `SOURCE_DATE_EPOCH`，保证同一 `cake.yaml` 两次 compose 得到相同 manifest digest。

### 2.6 自动分层

默认开启，`--no-split` 禁用。目标：单个文件变化只影响一层；避免单 blob 超过 registry 上限。

规则（按路径排序后应用），阈值 `large_threshold` 默认 **5 GiB**：

1. 大小 ≥ 5 GiB 的文件**独占一层**。
2. 大小在 `[small_threshold, 5 GiB)` 的"中等"文件（`small_threshold` 默认 64 MiB）按路径顺序装箱，每箱不超过 5 GiB。
3. 小于 `small_threshold` 的小文件单独装箱（同样 5 GiB 上限，实际通常只有一箱）。

中小文件分开装箱是为了稳定性：修改 `config.json` 不会导致与之同箱的 4.9 GiB 分片重传。对典型权重目录（N 个 ≤ 5 GiB 的 safetensors 分片 + KB 级 json/tokenizer），结果是约 N 层分片 + 1 层杂项；两个 4.9 GiB 分片装不进同一箱，天然一层一个。

type 可以在此基础上追加"必须单独成层"的子树（见 2.7 `Layout`），如 venv 的 `.python/`。

### 2.7 Type 体系

**type 是 `pack` 阶段的概念**：决定一个目录如何被检查、整理、分层、标注，以及附带哪些元数据。`compose` 不依赖具体 type——它只读通用注解（`layer.root`、`layer.overlay`、`layer.requires`）；遇到不认识的 type 照常组合，只是跳过该 type 的专有检查并给出 warning。这是"后续能加 type 而不破坏已有镜像/旧版本 CLI"的保证。

#### 2.7.1 接口

每个 type 是 `pkg/types` 中的一个实现，通过 `types.Register()` 注册；库使用者可注册自己的 type。

```go
type Type interface {
    Name() string                       // "venv"，同时是注解命名空间 com.github.baba.sandwichcake.<name>.*
    Schema() int                        // 专有注解格式版本，写入 layer.schema
    Options() []Option                  // 本 type 的参数声明；CLI 据此生成 `swcake pack <type>` 的 flags
    Defaults() Defaults                 // 压缩算法/等级、是否分层、阈值 的默认值
    Prepare(ctx, *Source, Opts) error   // 打包前对源目录的检查与整理：校验结构、rewrite-prefix、生成清单
    Layout(*Source) []Rule              // 分层提示：哪些子树单独成层、哪些文件排除
    Annotate(*Source) map[string]string // 专有注解 + layer.requires 的值
    Metadata(*Source) []MetadataFile    // 附属元数据文件 → metadata artifact 的 blob
    Check(ctx, *Slice, *ComposeCtx) []Finding  // compose 时的专有校验（可选，返回 warn/error）
}
```

- `Prepare` 可以修改源目录的副本（如重写 `pyvenv.cfg`），不动用户原目录。
- `Layout` 的规则叠加在 2.6 的通用分层规则之上，只能"更细"（强制拆分），不能合并。
- `Check` 由 `compose` 在能识别该 type 时调用；不识别则跳过。专有检查之外，`layer.requires` 由框架统一校验，type 只负责填值。
- 新增 type 的工作量 = 实现该接口 + 文档，不改框架；专有注解升级时 `Schema()` 加一，旧 schema 的解析由 type 自己兼容。

#### 2.7.2 内置 type

| type | 用途 | root 内容约束 | 默认压缩 | Layout | 专有注解 `com.github.baba.sandwichcake.<type>.*` | 元数据 artifact | Check |
|---|---|---|---|---|---|---|---|
| **`venv`** | Python 虚拟环境 | 必须有 `pyvenv.cfg`、`bin/python`、`lib/python3.X/site-packages`；embedded 模式下有 `.python/`（见 2.4） | zstd | `.python/` 单独成层 | `python`（版本）、`python.mode`、`python.home`、`lock.kind`、`lock.digest` | lock 文件全文、`pip freeze` 输出 | embedded：`requires=glibc>=X` 由框架校验；system：`python.home` 所指解释器须在 base 中（能判断时） |
| **`weight`** | 模型权重 | 任意；识别 `*.safetensors`/`*.gguf`/`*.bin`/`*.pt` 统计规模 | none | 无额外规则 | `source`（`hf://org/name@rev`、`https://`、`s3://`）、`format`（safetensors/gguf/…）、`files.digest`（清单 digest） | `files.json`：每个文件的路径/大小/sha256；有则附 HF `model_index`/`config.json` 副本 | 无 |
| **`binary`** | 预编译程序/库（uv、CUDA 用户态库、自编译工具） | 任意；扫描 ELF 头提取 arch 与所需 glibc 版本 | gzip | 无 | `arch`、`elf.interp` | 无 | `requires=glibc>=X` 由框架校验；`arch` 与 target platform 必须一致 |
| **`patch`** | 对 base 的覆盖式修改（配置替换、hotfix） | 任意；**唯一设置 `layer.overlay=true`** 的 type | gzip | 无 | `base`（预期覆盖的 base `repo@digest`，可空） | 无 | `base` 非空且与实际 base 不符 → error |
| **`other`** | 兜底：任意文件 | 任意 | gzip | 无 | 无 | 无 | 无 |

`base` 与 `external` **不是 type**：它们是层的来源分类（2.1），没有 `pack` 阶段，层上也没有 `layer.type` 注解（external 层只有 `source.*`）。

#### 2.7.3 候选 type（不在第一阶段，但接口要容得下）

- `cuda`：CUDA toolkit / cuDNN / TensorRT 用户态库，`requires` 带驱动版本下限，`Check` 对比 base 的 `NVIDIA_REQUIRE_CUDA` 环境变量。
- `hf-cache`：Hugging Face 缓存目录布局（`models--org--name/snapshots/<rev>`），比 `weight` 多一层目录结构校验与去重。
- `conda`：conda/mamba 环境，与 venv 类似但 Prepare 要处理 `conda-meta` 与硬编码前缀。
- `node`：`node_modules` + 自带 Node 二进制，与 venv 同构。

## 3. 传输策略与 plan

每一层在推送前归入三条路径之一：

| 情况 | 策略 | 客户端流量 |
|---|---|---|
| 目标 repo 已有该 digest | **reuse**：只写 manifest | 0 |
| 同一 registry 不同 repo | **mount**：cross-repo blob mount（`POST .../blobs/uploads/?mount=&from=`，`remote.Write` 自动处理，需来源 repo 的 pull 权限） | 0 |
| 跨 registry | **copy**：流式中转，不落盘 | 全量 |

- mount 的前提是层对象携带来源 repo 信息：ggcr 中 `remote.Image` 返回的层实现了 `remote.MountableLayer`，`remote.Write` 据此自动尝试 mount。实现时**任何对层的包装（改 mediaType、加注解）都必须透传该接口**，否则会静默退化为 copy。plan 阶段用 `HEAD /v2/<target>/blobs/<digest>` 预判 reuse。
- `compose --dry-run` / `plan` 输出每层的策略与字节数，形如 `terraform plan`。
- 出现 **copy** 时必须二次确认（交互式提示，列出总字节数）；`--yes` 跳过。非 TTY 且无 `--yes` 直接失败。
- 大 blob 上传使用分块 `PATCH`，支持断点续传（go-containerregistry 目前为整流上传，需在 `registry` 包自行实现）。
- 本地缓存 `~/.cache/sandwichcake/`：只缓存元数据（digest ↔ diff_id、manifest、tar 头清单），不缓存 blob。

## 4. 组合语义

### 4.1 `cake.yaml`

设计原则：

- `compose` **只做组合，不做上传**。`cake.yaml` 中一切输入都是 registry 引用（`ref`），没有本地路径；本地内容先 `pack` 成 slice 再引用。这样 `compose` 的输入全部可寻址、可锁定、可复现。
- `base` 和 `slices[]` 的每一项都是同一种结构 **`Ref`**；`ref` 必填，其余字段可选。
- 文件分四个顶层块，各管一事：`target`（输出到哪）、`base`（第一段）、`slices`（后续段）、`config`（OCI config patch）。

```yaml
apiVersion: sandwichcake.io/v1
kind: Cake

target:
  ref: registry.example.com/team/llm-serving:v3
  platforms: [linux/amd64, linux/arm64]   # 可省略，默认跟随 base
  annotations:                            # 最终 manifest 级注解，可省略
    org.opencontainers.image.source: https://github.com/team/llm-serving

base:
  ref: nvidia/cuda:12.4.1-runtime-ubuntu22.04

slices:
  - ref: registry.example.com/shared/venv/torch-cu124:2.4.0
  - ref: registry.example.com/weights/meta-llama--Llama-3.1-8B-Instruct@sha256:…
  - ref: ghcr.io/foo/bar:1.2
    layers: [3, "sha256:…"]
    platform: any
    force: true

config:
  env:
    VIRTUAL_ENV: /opt/venv
    PATH: { prepend: /opt/venv/bin }
  entrypoint: ["python", "-m", "server"]
  cmd: []
  workdir: /app
  user: "1000:1000"
  labels:
    com.example.team: serving
```

**`Ref` 结构**：

| 字段 | 必填 | 含义 |
|---|---|---|
| `ref` | 是 | `repo:tag` 或 `repo@digest`。解析后按 manifest 注解 `com.github.baba.sandwichcake.kind=slice` 判定是 **slice** 还是 **普通镜像** |
| `layers` | 否 | 选择层：下标（从 0 起，按 manifest 顺序）或 `sha256:` digest。slice 省略时取全部层；普通镜像**必须**给出，否则报错（整体引入普通镜像只能作为 `base`） |
| `platform` | 否 | `linux/amd64` 等：ref 为 index 时选哪个 manifest（默认跟随 target 的各平台逐个匹配）。`any`：声明这些层架构无关，在 target 所有平台上复用同一 blob |
| `force` | 否 | 普通镜像的层未通过 2.1 的纯文件判定时仍然放行；对 slice 无意义 |
| `verify` | 否 | 下载层扫 tar 头做 whiteout/路径重叠检查（见 2.1 第 3 条） |
| `name` | 否 | 仅用于 plan/inspect 输出的可读名 |
| `libc` | 否 | 仅 `base`：显式声明 base 的 libc（`"glibc 2.35"` / `"musl"`），在 libc 解析中优先级最高（见 9） |

`base` 的 `Ref` 不允许 `layers` / `force`（总是整体引入），其余字段同上。

**`cake.lock`**：`compose` 成功后生成，结构与 `cake.yaml` 同形，但所有 `ref` 都替换为 `repo@digest`，并记录每一项解析到的层 digest 列表与 target 的最终 digest。`compose --locked` 只读 lock、不解析 tag。每项另保留原始 tag ref（`input:`），使 `org.opencontainers.image.base.name` 等注解在 `--locked` 重放间保持稳定。

### 4.2 config 合并规则

- 起点是 base 的 config。
- `env`：按 key 覆盖；支持 `{prepend|append: ...}` 用于 `PATH`、`LD_LIBRARY_PATH` 一类。OCI 的 `Env` 是 `KEY=VALUE` 字符串数组、运行时不做 `$VAR` 展开，所以 prepend/append 在 compose 时用 base 的实际值拼成字面量写入。
- `entrypoint`/`cmd`/`workdir`/`user`：显式给出则覆盖，否则保留。
- `labels`：合并。
- `history`：base 的 history 原样保留，再追加每个 slice 的 history 条目，保证非 `empty_layer` 条目数与 `diff_ids` 数相等（base 本身不对齐时 warn，不修复）。
- `rootfs.diff_ids`：base 全部 diff_id + 各 slice 层的 diff_id，顺序即 `slices` 顺序。
- Docker 专有字段（`Healthcheck`、`Shell`、`OnBuild`）不在 OCI 规范中但 Docker 从 OCI config 中照读，原样保留。

### 4.2.1 媒体类型归一化

base 可能是 Docker schema2 manifest（`application/vnd.docker.distribution.manifest.v2+json`，层为 `vnd.docker.image.rootfs.diff.tar.gzip`）。最终镜像一律输出 **OCI manifest**（需要 `annotations`、zstd、未压缩层，这些 schema2 都没有），base 的层描述符改写为对应的 OCI 类型（`+gzip` ↔ `tar.gzip`，blob 与 digest 不变，只改 descriptor 的 `mediaType`），config 重新序列化为 `vnd.oci.image.config.v1+json`。ggcr 的 `mutate.MediaType` / `mutate.ConfigMediaType` 即为此用。Foreign layer（`vnd.docker.image.rootfs.foreign.diff.tar.gzip`，带 `urls`，Windows base 镜像专用）直接拒绝——与"不支持 Windows 容器镜像"一致。

一个 OCI manifest 内允许各层使用不同的压缩（gzip base + 未压缩权重 + zstd venv），Docker ≥ 23 / containerd / CRI-O 均支持；若目标运行时含 Docker 20.x，`compose --compat docker20` 禁止 zstd 层出现。

### 4.3 校验（compose 前，纯元数据，不下载 blob）

- 路径冲突：file 层之间 `layer.root` 不得互为前缀（`layer.overlay=true` 的层跳过）；diff_id 完全相同的层视为同一层去重，不算冲突。与 base 的冲突（如 root 在 base 中已是普通文件）仅元数据无法发现，`--verify` 时读 base 层 tar 头检查。
- venv（embedded）：`venv.libc` 与 base 的 libc 兼容；venv（system）：`venv.python.home` 所指解释器须存在于 base（通过 base 注解或用户声明；无法判断时 warn）。
- external：按 2.1 的纯文件判定。
- 平台：每个 platform 都必须能解析到对应 slice；`platform: any` 的 slice 在所有平台共享。

### 4.4 多架构

- `base` 为 image index 时，按 `platforms` 逐个解析；输出 image index。
- slice 本身可以是单 manifest 或 index；`pack --platform` 可标注。
- 架构无关层（权重、配置）在所有平台的 manifest 中引用同一个 blob，不会重复传输。

## 5. 命名与发现

### 5.1 OCI 能力边界

- **列一个 repo 的所有 tag**：有标准 API `GET /v2/<name>/tags/list`（分页 `?n=&last=`），所有 registry 都支持。gcr 对应 `remote.List`。
- **列一个前缀下的所有 repo**：标准只有 `GET /v2/_catalog`，返回全局所有 repo 且**不可按前缀过滤**，Docker Hub / GHCR 不开放，Harbor / ECR / GAR 各有私有 API。GAR/GCR 有扩展：`tags/list` 响应里带 `child` 字段列出子 repo。**不可移植，不能依赖。**

### 5.2 设计决策

- sandwichcake 的发现机制只依赖 `tags/list`：用户在配置中声明若干 **slice repo**（`~/.config/sandwichcake/config.yaml` 的 `repos:`），`sandwichcake ls` 遍历这些 repo 的 tag，拉 manifest，按 `com.github.baba.sandwichcake.kind=slice` 过滤。
- 对支持 `_catalog` 或 GAR `child` 的 registry，提供 `sandwichcake discover <prefix>` 作为可选便利功能，不支持时明确报错。
- 配置分两级：全局 `~/.config/sandwichcake/config.yaml`（环境类：cache、upload、`repos`）与项目根的 `cake-config.yaml`（输出类：registry 前缀、命名模板、pack 默认值；`swcake init` 生成，从 cwd 向上查找项目根）。`swcake config pull/push` 把整个项目树（`cake/*.yaml`、lock 等）同步到 `oci://` artifact 或 git 远端。具体 schema 不在这里展开。

### 5.3 权重 slice 的命名

两种方案对比：

| | `user/weights/<modelid>:<tag>`（repo-per-model） | `user/weights:<modelid>-<info>`（单 repo） |
|---|---|---|
| 可移植性 | Docker Hub 只允许 2 段路径、Quay 只允许 org/repo，**不可用**；GHCR / GAR / Harbor / ECR 可用 | 所有 registry 可用 |
| tag 语义 | 干净：tag = revision / 量化变体 | tag 混入模型 id，128 字符上限吃紧 |
| 权限 / GC | 按模型粒度 | 整个权重库一体 |
| 跨模型共享 blob | 依赖 mount | 同 repo 天然去重 |

决策：**repo-per-model**，不提供 flat 模式。模型 id 的 `/` 映射为 `--`（沿用 Hugging Face 缓存目录 `models--org--name` 的惯例）：

```
registry.example.com/user/weights/meta-llama--Llama-3.1-8B-Instruct:5206a32
registry.example.com/user/weights/meta-llama--Llama-3.1-8B-Instruct:5206a32-fp8
```

后果：Docker Hub、Quay 不能作为权重 slice 的存放地（路径段数限制），文档中明示。用户仍可手工指定任意 `--to`，命名规则只作用于自动生成。

真实模型 id 与 revision 始终写在 `com.github.baba.sandwichcake.weight.source` 注解中，名字只是寻址手段。`pack weight --source hf://org/name@rev` 可自动生成目标名。

### 5.4 Tag 可变性

tag 可被覆盖。`cake.lock` 记录每个 ref 解析到的 digest；`compose --locked` 只用 lock 中的 digest；最终镜像的 `com.github.baba.sandwichcake.slices` 注解也只记 digest。

## 6. CLI

二进制名 `sandwichcake`；同时安装短别名 **`swcake`**（符号链接）。候选对比：

| 别名 | 长度 | 辨识度 | 冲突 |
|---|---|---|---|
| `swcake` | 6 | 与全名直接关联，`sw<Tab>` 可补全 | 未发现 |
| `hoagie` | 6 | 高（hoagie 即三明治），但非英语母语用户不易联想 | npm 上有同名 JS 库（非 CLI） |
| `sck` | 3 | 低，像随机缩写 | 若干小众 GitHub 项目 |
| `cake` / `swc` / `stack` / `slice` | — | — | CakePHP、Cake build、swc 编译器、Haskell stack；`slice` 过于通用 |

选 `swcake`：和全名一眼对应、无冲突。文档中示例一律用 `swcake`。

`pack` 按 type 分子命令，每个子命令的 flags 由对应 `Type.Options()` 生成，因此新增 type 自动获得 CLI：

```
swcake pack <type> <dir> --to <repo:tag> --root <path> [通用 flags] [type flags]
  通用 flags: [--compression gzip|zstd|none] [--level N] [--no-split]
              [--large-threshold 5Gi] [--small-threshold 64Mi] [--platform linux/amd64|any]
              [--rewrite-prefix <src>=<root>] [--owner uid:gid] [--no-parents] [--artifact-compat]
  swcake pack venv   ./venv   --to r/venv/torch:2.4 --root /opt/venv [--python embedded|system] [--lock uv.lock]
  swcake pack venv   --from-lock uv.lock --python 3.12 --to … --root /opt/venv     # 调用本机 uv 生成后打包
  swcake pack weight ./llama  --to r/weights/meta-llama--Llama-3.1-8B:5206a32 --root /models/llama --source hf://…
  swcake pack weight hf://org/name[@rev] --to …     # 远端模型（hf://、modelscope://、https://）流式入 registry，不落盘
  swcake pack binary ./uv     --to r/bin/uv:0.5 --root /usr/local/bin
  swcake pack patch  ./etc    --to r/patch/nginx-conf:1 --root /etc/nginx [--base <repo@digest>]
  swcake pack other  ./conf   --to r/misc/conf:1 --root /etc/serving

swcake compose  -f cake.yaml [--dry-run] [--yes] [--locked] [--verify] [--compat docker20]
swcake plan     -f cake.yaml                 # 等价 compose --dry-run
swcake inspect  <ref>                        # 层、type、root、大小、来源、history、referrers 元数据
swcake ls       [repo...]                    # 列出 slice（默认用配置中的 repos）
swcake discover <prefix>                     # 需 registry 支持 _catalog / GAR child
swcake tag      <ref> <new-tag>              # 纯 manifest 操作，支持 mount 到其他 repo
swcake diff     <ref-a> <ref-b>              # 层级差异、可复用情况
swcake rm       <ref>                        # 删 manifest/tag 及其 referrers；layer 由 registry GC 回收
swcake types                                 # 列出已注册 type 及其 flags
swcake init     [dir]                        # 初始化项目（cake-config.yaml、cake/ 等），不覆盖已有文件
swcake config   <sub>                        # show/set/get/unset 查改用户默认；init --global 建全局配置；pull/push 同步项目树（oci:// 或 git）
swcake version                               # 打印版本
swcake completion <shell>                    # 生成 shell 补全脚本
```

上传只发生在 `pack`（slice + metadata artifact）和 `compose`（最终 manifest，以及必要的 mount/copy）；`compose` 不会从本地目录生成任何层。

关于"管理 layer"：Registry API 没有删除 blob 的操作，只能删 manifest（`DELETE /v2/<name>/manifests/<digest>`，且 Docker Hub 不开放该端点，需走其私有 API）。`rm` 只做 tag/manifest 删除，并提示哪些层将变为无引用；是否回收取决于 registry 的 GC 策略。

## 7. 代码结构

```
cmd/sandwichcake/        CLI（cobra）
pkg/
  layer/                 确定性 tar、分层策略、压缩（gzip/zstd/none）、通用注解
  types/                 Type 接口、Register()、Option 声明
  types/venv, weight, binary, patch, other/   内置 type，各自独立包
  slice/                 slice manifest 的构造与解析、pack 流程（本地 + 远端流式，调用 types）、metadata artifact
  fetch/                 远端模型下载：provider API（HF/ModelScope）+ HTTP 客户端（ranged stream、断点续传）
  cake/                  cake.yaml / cake.lock 的 schema、解析、校验
  resolve/               Ref → 层描述符列表（slice 全部层 / 普通镜像选层 + history 判定 / base；含 libc 探测）
  compose/               纯函数：base + slices + config patch → v1.Image / v1.ImageIndex，不做 I/O
  plan/                  计算每层 reuse/mount/copy 与字节数
  runner/                编排：resolve → compose → plan → 确认 → push → 写 lock
  registry/              对 ggcr remote 的封装：auth、重试、分块上传、断点续传、tags/list、catalog
  inspect/               读取并展示
  version/               版本信息（构建时注入）
internal/
  cache/                 元数据缓存
  testutil/              共享测试辅助（fake registry、ELF 构造等）
  doctest/               文档链接检查（仅测试）
  releasetest/           release workflow 契约测试（仅测试）
```

`compose` 不碰网络，输入为已解析的描述符，便于单元测试。`v1.Image` 接口是 lazy 的，只在真正推送时才读取 blob。

## 8. 已决定的边界

- 不做镜像构建。
- 不支持 Windows 容器镜像；Windows 客户端需处理权限位（统一 0644/0755）、符号链接（可能需要特权，失败则报错）、路径分隔符。
- 不保留来源镜像的签名（签名绑定 manifest digest）；通过 `com.github.baba.sandwichcake.slices` / `com.github.baba.sandwichcake.source.image` 提供溯源，用户可对新 manifest 自行签名。
- slice 与最终镜像都是 image manifest；OCI 1.1 artifact / referrers 只用于附属元数据，权重本身不做成 artifact（保证 `docker run` 直接可用）。
- 不内置 uv / Python 下载逻辑，`pack venv --from-lock` 依赖本机 uv。

## 9. 待定

- 【已定】base 的 libc 版本：base ref 的 `libc:` 字段优先（见 4.1），其次 `com.github.baba.sandwichcake.libc` manifest 注解或 config label，否则 `--verify` 扫 base 层 tar——先按文件名（`ld-musl-*` → musl、`libc-X.Y.so` → glibc X.Y），再读 `libc.so.6` 内容中的 "GNU C Library ... version X.Y"；后层覆盖前层。三者皆无则 libc 为 ""，对不可校验的 requires 只 warn。
- 5 GiB 单层上限需在 Harbor、ECR、GHCR、GAR 上验证可接受（部分 registry 对单 blob 有 ~10 GiB 软限制，5 GiB 应安全，但要实测分块上传超时）。
- 旧 registry 对 `subject` / `vnd.oci.empty` 的兼容矩阵，决定 `--artifact-compat` 是否需要自动探测。
