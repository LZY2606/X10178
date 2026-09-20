# 峰谷罗盘 (Peak–Valley Compass)

本地色谱批次对齐工具：导入每次运行的时间/信号与样本、仪器元数据，检测候选峰，
通过研究员指定的跨运行锚点建立**单调时间映射**，并在不抹掉真实保留时间漂移的
前提下把多个运行对齐到参考运行、合并为批次共识。

## 运行

```bash
go build ./...
go test ./... -count=1
go run ./cmd/server --addr 127.0.0.1:5238
# 打开 http://127.0.0.1:5238 ，页面标题为“峰谷罗盘”
```

首次打开点击右上角 **载入演示数据**：自动导入 3 个确定性运行（参考、漂移且缺峰
的批次 B、非均匀采样的批次 C），检测候选峰（含平台峰/并列候选），并给出锚点建议。

## 数据与溯源

- 原始 `(time, signal)` 以内容寻址 blob 存于数据目录 `blobs/`，SHA-256 摘要入库，永不重写。
- 派生对齐曲线保存对齐后的参考时间轴，并记录 `source_digest`（原始采样）与
  `map_anchors_hash`（映射版本），任何派生点都可回溯到原始采样点和映射版本。
- 元数据（运行、检测、编辑版本、锚点、计划、映射、锁、峰族、共识）在 SQLite
  （WAL）：`peakvalley.db`。
- 未冻结草案与冻结结果都持久化，重启后原样恢复；冻结后不可再修改。

## 关键设计

- **候选峰身份**：每个峰在检测时获得稳定 ID，重新检测用边界 IoU 的身份迁移
  （same/moved/split/merged/inserted/deleted），不依赖数组下标。
- **平台峰 / 并列候选**：平台峰顶与半高边界来自原始采样，显著性用平滑序列计算；
  显著性在容差内相等的候选被标记为 `tied`，不强制排序。
- **交叉锚点**：保存映射前逐运行校验，逆序与“同峰重指”都会报冲突；用最长非递减
  子序列给出**最小拒绝锚点集合**，绝不靠全局排序掩盖。平台峰上两个真正不同的锚点
  允许同一运行时间。
- **部分参与**：锚点可以只出现在部分运行中（如批次 B 缺峰），缺失在共识中显式列出。
- **单调映射**：分段线性，区间内斜率非负；锚点之外以 slope=1 外延，保留真实漂移。
- **局部重算 / 锁定区段**：调整锚点只影响相邻区段；锁定区段按其时间区间几何定义，
  重新构建后必须逐点一致，改变锁定区间的编辑会被拒绝。
- **自动建议**：每个运行峰保留多个参考候选，按时间/形状/面积/邻峰关系打分并给出
  文字说明；“单调贪心”只标记建议选择，不自动落库。
- **人工合并 / 拆分 / 忽略**：形成追加式、可撤销版本，撤销追加补偿版本而非改写历史。
- **峰族与共识**：单连接聚类；每个共识峰列出支持与缺失运行；面积归一化规则
  （TIC / 峰面积中位数 / 不归一化）固定在共识版本中。
- **并发**：锚点草案、峰族、共识都带 `rev`/`expect_rev` 乐观并发，旧版本提交返回 409。

## API 摘要

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/runs` | 导入运行（时间、信号、元数据） |
| POST | `/api/runs/{id}/detect` | 检测 / 重新检测候选峰（参数固定到检测版本） |
| POST | `/api/runs/{id}/edits` | 合并 / 拆分 / 忽略 / 恢复 |
| POST | `/api/runs/{id}/undo` | 撤销到指定版本之前（追加版本） |
| GET | `/api/runs/{id}/migrations` | 重检测身份迁移 |
| POST | `/api/alignment` | 建立对齐工作区（指定参考运行） |
| POST | `/api/alignment/anchors` | 新建锚点（含每运行参与峰） |
| POST | `/api/alignment/anchors/{aid}/points` | 拖动 / 重指某运行的峰（可清空=缺失） |
| GET | `/api/alignment/suggest/{rid}` | 多候选自动建议与说明 |
| POST | `/api/alignment/build` | 校验单调性并保存新映射版本 |
| POST | `/api/alignment/lock` | 锁定映射区段 |
| POST | `/api/alignment/freeze` | 冻结映射（不可变） |
| POST | `/api/runs/{id}/derive` | 生成可溯源对齐曲线 |
| GET | `/api/runs/{id}/residuals` | 锚点移位与局部 chord 残差 |
| POST | `/api/families/build` | 峰族单连接聚类（容差可设） |
| POST | `/api/families/merge` / `ignore-member` / `freeze` | 峰族人工编辑 / 冻结 |
| POST | `/api/consensus/build` / `freeze` | 批次共识（归一化规则随版本固定） / 冻结 |
| POST | `/api/demo/seed` | 一键确定性演示数据 |

## 代码布局

- `internal/model` — 领域类型
- `internal/dsp` — 峰检测、平台/并列、身份迁移、合并/拆分/忽略物化
- `internal/align` — 冲突与最小拒绝集（LNDS）、单调分段映射、局部重算与锁、建议
- `internal/consensus` — 峰族聚类与批次共识/归一化
- `internal/store` — SQLite schema/仓储与内容寻址 blob
- `internal/app` — 事务编排、血缘校验、乐观并发
- `internal/webapi` — HTTP API 与嵌入的单页前端
- `web/assets` — 前端资源说明（实际页面随 `internal/webapi/static` 嵌入）
