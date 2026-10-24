# 买不买 · 投票功能的数据库知识清单

> 需求 3 第 ② 步（repo 层 `PurchaseRepo.Vote`）整理，2026-10-13。
> 代码：`internal/repo/purchase/purchase_repo.go` 的 `Vote`；测试：`internal/repo/repo_test/purchase_vote_repo_test.go`、`purchase_vote_concurrency_test.go`。

## Vote 的流程

```
① 开事务（DB.Transaction）
② SELECT … FOR UPDATE 锁住帖子行 → 不存在 / 已关闭就返回哨兵错误（自动回滚）
③ 查这个人原来的票
   选项没变 → 直接返回
④ 写票：没投过就插入，投过就只改 choice
⑤ 改计数：Incr 新选项；改票时 Decr 旧选项
⑥ CAS 关闭：UPDATE … SET vote_closed = true
            WHERE vote_closed = false AND buy_count + wait_count + skip_count >= closeAt
   受影响行数 == 1 → justClosed
⑦ 用新的空结构体重新读一次，返回最新票数
事务提交，锁释放 → service 根据 justClosed 发通知（不在事务里发）
```

## 一、事务

| 知识点 | 要解决的问题 | 解决方案 | 在本项目 |
|---|---|---|---|
| 事务 / 原子性 | 写票成功、改计数失败，数据对不上 | `BEGIN … COMMIT`，出错时 `ROLLBACK` 全部撤销 | `Vote` 整个包在 `DB.Transaction(...)` 里，闭包返回 error 自动回滚，返回 nil 自动提交 |
| 自动提交 | 以为没写 BEGIN 就「不在事务里」 | 每条语句自己就是一个事务；多条语句要绑在一起，必须显式开事务 | 实验里 INSERT 和 UPDATE 拿到了两个连续的事务号 `xmin` 64528 和 64529 |
| 事务要短 | 锁一直持有到事务结束，长事务让别人一直等 | 事务里只放数据库操作 | `Vote` 只做 SQL；不能在事务里调 LLM 或外部接口 |
| 出错时的返回值 | 事务回滚了，却还告诉调用方「关闭了」 | 出错路径一律返回零值 | 出错时返回 `justClosed = false`，避免发假通知 |
| 通知放在提交之后 | 事务里发了通知，事务又回滚了 | repo 只返回结果，service 在提交后再发 | 和 `answer_repo.go` 先 `Transaction` 再 `sendAcceptAnswerNotification` 一样 |

## 二、MVCC 与隔离级别

| 知识点 | 要解决的问题 | 解决方案 | 在本项目 |
|---|---|---|---|
| MVCC 多版本 | 读写互相阻塞，性能差 | 每次 UPDATE 写一个新版本，读的人按快照只看已提交的版本 | 实验：A 没提交时 B 读到 0，而且没被卡住 |
| 行级的版本 | 以为每次修改都会复制整张表 | 只有被改的那一行产生新版本，而且是**整行复制**（大字段走 TOAST，只复制指针） | 实验：只有 id=1 的 `ctid` 从 `(0,3)` 变成了 `(0,5)` |
| 快照是什么 | 以为每个人手里有一份数据副本 | 快照只是「此刻哪些事务已提交」的名单，逐行检查 `xmin` / `xmax` 决定是否可见 | — |
| PG 和 MySQL 的差异 | 面试常问 | PG：旧版本留在表里，由 VACUUM 清理；MySQL InnoDB：原地修改，旧值写进 undo log，由 purge 清理 | — |
| 死元组 / VACUUM | 热点行频繁更新，产生大量垃圾版本 | autovacuum；热点计数改用 Redis 计数或拆成多行 | 一个帖子最多 10 票，没有影响 |
| READ COMMITTED | 不知道自己能看到什么 | PG 默认级别，**每条语句**拿一个新快照，只能读到已提交的数据 | 我们用的就是它，配合行锁 |
| REPEATABLE READ / SERIALIZABLE | 需要更强的隔离 | 整个事务用同一个快照；冲突时报 40001，应用层需要重试 | 没用到，行锁已经够了 |
| 不写 ORDER BY 的顺序 | 以为结果有固定顺序 | 不写 ORDER BY 就按物理位置读，UPDATE 后顺序会变 | 实验：UPDATE 后 id=2 排到了前面 |

## 三、并发问题与锁

| 知识点 | 要解决的问题 | 解决方案 | 在本项目 |
|---|---|---|---|
| 丢失更新 | 读出来、在代码里加 1、再写回去，两人同时投只加了 1 | 原子写法 `SET col = col + 1` | 第 ⑤ 步用 `Incr` / `Decr` |
| 等锁之后重新检查 | 担心原子加 1 用的也是旧值 | READ COMMITTED 下，UPDATE 等到锁后，会在**最新提交的版本**上重新计算和判断 WHERE | 实验 3 第二遍结果正确 |
| 先检查再动手（check-then-act） | 两人都读到「没关闭」，第 11 票混了进来 | `SELECT … FOR UPDATE`：读最新版本并加锁，别人排队 | 第 ② 步锁住帖子行；实验 4 里 B 等锁后读到了 2 |
| FOR UPDATE 挡谁 | 以为会把所有读都锁住 | 只挡 `FOR UPDATE` 和写操作；普通 SELECT 不受影响（PG 里普通 SELECT 永远不会被卡住） | — |
| 锁持有到事务结束 | 想提前释放锁 | 做不到（两阶段锁），所以事务要短 | 第 ⑦ 步重新读时不用再加 `ForUpdate` |
| 锁不存在的行 | 第一次投票时投票行还不存在，锁不住 | PG 没有间隙锁 → **锁父行**，再用唯一约束兜底（MySQL InnoDB 有间隙锁） | 锁的是 `purchase_detail` 而不是 `purchase_vote` |
| 死锁 | 两个事务按相反的顺序加锁，互相等待 | 所有代码按同样的顺序加锁；PG 检测到死锁会让其中一个报错 | 永远先锁帖子这一行 |
| CAS（比较并交换） | 关闭和通知只能发生一次 | 「检查条件」和「修改」放进同一条 `UPDATE … WHERE`，看受影响的行数 | 第 ⑥ 步，`affected == 1` 才算 `justClosed` |
| 受影响行数的差异 | MySQL 统计值真正改变的行，PG 统计匹配上的行 | 让 WHERE 保证匹配上的行，值一定会变 | `vote_closed` 从 false 改成 true，两种数据库都数出 1 |
| 排查锁等待 | 线上请求卡住，不知道原因 | `pg_stat_activity` 看状态，`pg_blocking_pids(pid)` 看是谁挡住了 | 实验时看到了 `idle in transaction` 和等待 `Lock` |
| 忘记结束的事务 | `idle in transaction` 一直持有锁 | `DB.Transaction(func...)` 保证函数返回时一定提交或回滚 | — |

## 四、约束、索引与连接

| 知识点 | 要解决的问题 | 解决方案 | 在本项目 |
|---|---|---|---|
| 复合主键 | 同一个人重复投票 | `PRIMARY KEY (question_id, user_id)`：一个主键由两列组成，唯一的是两列的组合 | v37 测试验证了第二次插入被拒绝 |
| 复合主键 vs 自增 id | 选哪种 | 自增 id + `UNIQUE(question_id, user_id)` 效果一样；表本来就靠「帖子 + 人」定位，复合主键少一列、少一个索引 | 用复合主键 |
| 最左前缀 | 复合索引什么时候能用上 | 按列的顺序从最左边开始匹配；只按 `user_id` 查用不上 | 查询都从帖子出发，`question_id` 放在前面 |
| 一个连接一个进程 | 连接开销大 | 连接池（Go 的 `database/sql` 自带）；多实例时加 PgBouncer | 上 k3s 时，多个副本的连接数加起来不能超过 `max_connections`（默认 100） |
| 并发模型对比 | 面试综合题 | PG 多进程 + 共享内存；MySQL 单进程多线程；Redis 单线程事件循环（epoll）；Go 用 goroutine | — |

## 五、迁移与 ORM（xorm）

| 知识点 | 要解决的问题 | 解决方案 | 在本项目 |
|---|---|---|---|
| 新装和升级两条路径 | 新装的库缺表 | `answer init` 走 `InitDB()`，按 `init_data.go` 的 `tables` 一次建好；`answer upgrade` 才逐条跑迁移。**新增表两处都要改** | 一开始漏了 `PurchaseVote`，问「新建库会不会跑迁移」时发现的 |
| 迁移什么时候跑 | 改了代码，表却没变 | 只有 `answer upgrade` 跑迁移，`answer run` 不跑；Docker 的 entrypoint 依次执行 `init → upgrade → run` | 本地用 `go run ./cmd/answer upgrade -C ./answer-data` |
| `Sync` 只增不减 | 想改列的类型 | `Sync` 只补缺少的表、列、索引；改类型要手写 SQL | v37 只加表、加列 |
| 迁移要能重复执行 | 迁移不在事务里，失败后会重跑 | 写成幂等的 | `TestAddPurchaseVote_idempotent` |
| 迁移测试从旧表开始 | 只在新库上测，发现不了加列的问题 | 先建出旧版本的表结构和数据，再跑迁移 | SQLite 不能加带 `CURRENT_TIMESTAMP` 默认值的列，测试数据要和真实的 v36 一致 |
| 零值被跳过 | `Update` 不写 0、false、"" | 用 `Cols(...)` 指定要改的列 | 改 choice、关闭投票都用了 `Cols` |
| 非零字段自动当条件 | 用填满数据的结构体去 `Get`，查不到最新数据 | 用新的空结构体，或者 `NoAutoCondition()`；同时检查 `exist` | 第 ⑦ 步的 `fresh`，测试抓到的 bug |
| 列名拼错不报错 | `Cols("chose")` 被静默忽略，只更新了 `updated_at` | 测试里断言具体的值；看 SQL 日志 | 测试抓到的 bug |
| `Get` 需要非 nil 指针 | `object should not be nil` | 先 `&entity.X{}` 再传给 `Get` | 删掉 `detail = &entity.PurchaseDetail{}` 后测试抓到的 |
| 对比 Prisma | 列名、条件都是字符串，没有类型检查 | 新项目可以选 sqlc（手写 SQL 生成类型安全代码）或 ent（类似 Prisma） | Answer 历史原因用 xorm，只能沿用 |

## 六、测试

| 知识点 | 要解决的问题 | 解决方案 | 在本项目 |
|---|---|---|---|
| 每个行为在它所在的层测 | 不知道测试该写在哪 | repo：真实数据库，测 SQL / 事务 / 锁；service：fake repo，测业务规则；controller：httptest | 锁和并发属于数据库，所以在 repo 层测 |
| repo 要用真实数据库 | fake 测不出 SQL 和锁的问题 | SQLite 或 dockertest 启动的 PG | `TEST_DB_DRIVER=postgres` |
| SQLite 不支持行锁 | 并发测试在 SQLite 上没有意义，还会报 `database is locked` | 只在 PG 上跑，其他情况 `t.Skip` | `requirePostgres` |
| 顺序测试证明不了锁 | 删掉 `FOR UPDATE`，顺序测试照样通过 | 并发测试 + 变异实验（删掉锁看测试失败） | `purchase_vote_concurrency_test.go` |
| goroutine 里不能用 require | `t.FailNow` 只能在测试自己的 goroutine 里调用 | 结果按下标写进切片，全部结束后再断言 | `runConcurrently` 的注释 |
| 空的测试也会通过 | 只写了 TODO 的 `t.Run` 显示 PASS | 用 `-v` 检查，确认每个用例都有断言 | — |
| 先判断是谁错了 | 测试失败就去改代码 | 先问「按规则，结果本来应该是什么」 | 第 10 票 `justClosed` 为 true 那次，是测试写错了 |
| 读错误信息 | 调用栈只告诉你错误被包装的位置 | 结合 SQL 日志，看事务里执行到了哪一条 | `BEGIN` 后直接 `ROLLBACK`，说明第一条 SQL 之前就失败了 |

## 七、Go 相关

| 知识点 | 说明 |
|---|---|
| context | 相当于 `AbortSignal` + 超时 + 请求级的值；有 ctx 就往下传，没有才用 `context.Background()` 新开一条链，并加上超时 |
| 哨兵错误 | `var ErrX = errors.New(...)`，返回时原样返回，调用方用 `errors.Is` 判断；不要和 pacman 的 `errors` 混用，标准库起别名 `stderrors` |
| 带类型的 nil | `*T` 类型的 nil 装进 `any` 后不等于 `nil`；测试用 `require.Nil`，函数返回「没有错误」时直接写 `return nil` |
| `int` 和 `int64` | 大小可能相同，但类型不同，不能直接比较；`require.Equal` 要类型也相同，`EqualValues` 会先转换 |
| 闭包 | 闭包里可以直接修改外层变量，`detail` 和 `justClosed` 就是这样从事务里带出来的 |
| 命名返回值 | 会被初始化为零值：bool 是 false 可以直接用，指针是 nil 不能直接用 |
| `:=` | 左边至少有一个新变量时可以用，其余的会被复用 |

## 附：实验 SQL

两个终端都执行 `docker exec -it funbi-pg psql -U root -d answer`。

```sql
-- 准备
CREATE TABLE demo_vote (id int PRIMARY KEY, buy_count int NOT NULL DEFAULT 0);
INSERT INTO demo_vote VALUES (1, 0);

-- 实验 1：MVCC，读不阻塞写
-- A: BEGIN; UPDATE demo_vote SET buy_count = buy_count + 1 WHERE id = 1;
-- B: SELECT * FROM demo_vote;        → 0，没有卡住
-- A: COMMIT;
-- B: SELECT * FROM demo_vote;        → 1

-- 实验 2：看版本
SELECT xmin, xmax, ctid, * FROM demo_vote;

-- 实验 3：丢失更新
-- A、B 都 BEGIN 并 SELECT 读到 0
-- A: UPDATE demo_vote SET buy_count = 1 WHERE id = 1;
-- B: UPDATE demo_vote SET buy_count = 1 WHERE id = 1;   → 卡住
-- A: COMMIT;  B: COMMIT;                               → 结果 1，丢了一票
-- 换成 SET buy_count = buy_count + 1 再做一遍           → 结果正确

-- 实验 4：FOR UPDATE（规则：满 2 票不能再投）
-- A: BEGIN; SELECT buy_count FROM demo_vote WHERE id = 1 FOR UPDATE;   → 1
-- B: BEGIN; SELECT buy_count FROM demo_vote WHERE id = 1 FOR UPDATE;   → 卡住
-- A: UPDATE … + 1; COMMIT;
-- B: 返回 2 → 已满，ROLLBACK

-- 排查锁等待
SELECT pid, state, wait_event_type, query FROM pg_stat_activity WHERE datname = 'answer';
SELECT pg_blocking_pids(<pid>);
SELECT pg_backend_pid();

-- 清理
DROP TABLE demo_vote;
```
