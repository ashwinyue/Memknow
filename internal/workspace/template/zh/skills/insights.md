# Insights Skill — 感悟记录

本 skill 规范碎片化感悟的**快速记录、分类整理和回顾**。

适合捕捉那些稍纵即逝的想法：一句话的体会、踩坑后的反思、对某个问题的新认知、值得沉淀的经验片段。

---

## 数据文件结构

```
$WORKSPACE_DIR/memory/
└── insights/
    ├── index.md                  # 分类索引（每次新增后更新）
    └── YYYY-MM.md                # 按月归档，同月感悟写入同一文件
```

执行时必须使用绝对路径（`$WORKSPACE_DIR/...`）。

---

## 感悟分类

| 分类 | 适用内容 |
|------|---------|
| 工作 | 专业技能、业务洞察、职场经验 |
| 学习 | 知识积累、认知升级、方法论 |
| 成长 | 个人反思、习惯、心态、价值观 |
| 生活 | 日常感受、人际关系、兴趣爱好 |
| 综合 | 不好归类的零散想法 |

分类由 AI 自动判断，用户可纠正。

---

## 触发识别

| 用户说 | 触发动作 |
|--------|---------|
| 「记录一下」「有个感悟」「想法」「体会」「反思」 | → **快速记录** |
| 「看看我的感悟」「回顾一下」「之前记过什么」 | → **回顾** |
| 「关于 XX 有没有感悟」「搜一下」 | → **检索** |

---

## 功能一：快速记录

1. 用户说出感悟内容
2. AI 提炼一个简短标签（2-4 字，作为标题）
3. 自动判断分类（不确定时默认「综合」）
4. 写入当月文件，更新索引

### 感悟条目格式

```markdown
### [简短标题]

> **[分类]** · YYYY-MM-DD

[感悟正文，保留用户原始表达，AI 可适当润色但不改变意思]

---
```

### 写入流程

```bash
flock -x \"$WORKSPACE_DIR/.memory.lock\" -c "
  mkdir -p \"$WORKSPACE_DIR/memory/insights\"
  MONTH_FILE=\"$WORKSPACE_DIR/memory/insights/$(date +%Y-%m).md\"
  if [ ! -f ${MONTH_FILE} ]; then
    printf '# 感悟 — %s年%s月\n\n' \"$(date +%Y)\" \"$(date +%m)\" > ${MONTH_FILE}
  fi
  cat >> ${MONTH_FILE} << 'EOF'

### [简短标题]

> **[分类]** · YYYY-MM-DD

[感悟正文]

---
EOF
"
```

---

## 功能二：更新索引

每次新增感悟后，同步更新 `$WORKSPACE_DIR/memory/insights/index.md`：

```markdown
# 感悟索引

> 最后更新：YYYY-MM-DD

## 工作
- [YYYY-MM 标题](YYYY-MM.md#简短标题) — 一句话摘要

## 学习
- [YYYY-MM 标题](YYYY-MM.md#简短标题) — 一句话摘要
```

---

## 注意事项

- 所有文件读写使用 `flock -x "$WORKSPACE_DIR/.memory.lock"` 加锁
- `$WORKSPACE_DIR/memory/insights/` 目录按需创建（`mkdir -p "$WORKSPACE_DIR/memory/insights"`）
- 感悟文件写入后优先追加，不随意修改历史内容
