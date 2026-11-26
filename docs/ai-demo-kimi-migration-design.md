# 案例页扩展设计稿（原学校站风格延续）

## 1. 信息架构

```
首页（保持原卡片栅格）
├── 学校组（原有，不动）
└── 外贸组（新增 3 张卡片）
    ├── 客户邮件整理   /ai-demo/customer-email/
    ├── 报价模拟       /ai-demo/quotation/
    └── 订单校验       /ai-demo/order-validation/
```

三张外贸卡片与学校卡片同组件、同尺寸，仅在卡片右上加灰字标签「外贸·模拟」，图标用渐变区分。

## 2. 首页外贸卡片（复用原卡片结构）

```
[渐变图标] 客户邮件整理                     外贸·模拟
一封措辞含糊的客户邮件 → 结构化要点与待确认清单
[查看案例 →]（黑按钮）
```

- 三张卡片文案分别点出场景：邮件含糊需求、报价计算、PO 版本冲突。
- 图标用现有渐变：`linear-gradient(135deg,#ff8a3d,#ff5e8a 55%,#9b5de5)`，圆角方块 40×40 内嵌白色线性小图标（信封 / 计算器 / 清单）。

## 3. 案例页统一布局（三页同构）

自上而下五个区块，最大宽 720px 居中：

1. **面包屑 + 标题**：`← 返回首页`；H1 + 一句话场景说明；其下放**显著性声明条**（灰底圆角，非弹窗）：
   > 本页为业务模拟演示：数据为虚构，结果由人工编写预置，未连接任何真实业务系统，不代表客户真实成果。
2. **步骤条**：横向三段「① 原始材料 → ② 整理与校验 → ③ 人工接手」，当前页三段全亮但样式区分（①灰、②黑、③橙渐变描边），表达"AI 只做到②，③由人完成"。
3. **原始材料卡片**：白卡内等宽字体展示原文（邮件 / 成本表 / PO），默认折叠显示前 6 行，`<button>`「展开原文 / 收起」控制；卡底注明「来源：虚构贸易公司 X 的 800 个水瓶业务 · 可核对」。
4. **整理结果卡片**：结构化字段表（标签灰、值黑），冲突/不确定项用左侧 3px 橙色竖条 + `⚠ 待人工确认` 徽标标出。
5. **人工接手区**：黑底白字窄卡片，列出"接下来由人做的事"清单，无 CTA 转化按钮。

页脚复用原站页脚，不加营销横幅、不加指标收益数字。

## 4. 三页内容要点

**邮件页**：客户把 500 改为 800（黑 600 / 白 200），贸易术语 FOB 宁波；9 月 30 日仅为客户**到港目标**；客户未提供价格。整理结果中「数量=800」「价格=未提供 ⚠」「交期=客户期望，需核生产周期 ⚠」。

**报价页**：成本 18,080 元，汇率 7.20，目标毛利 20% → 单价 USD 3.93，总额 USD 3,144；客户还价 3.70（毛利 15.17%）单独标记「另批」。并列展示裸杯报价 18.80 含税 / 18.30 未税、白色 MOQ 500，注明「口径不同，不能直接比较」。**报价草稿与内部成本分区隔离**：成本表放独立灰底卡片，标「内部 · 不对外」。付款：30% 定金 USD 943.20，70% 尾款 USD 2,200.80；交期文案固定为「定金与图稿齐备后 25 个自然日生产，不承诺到港日期」。

**订单页**：PO Rev0 显示黑 20 箱 + 白 5 箱、每箱 40 共 1000 个、金额 3,930——算术自洽但与 800 个报价冲突，且条款变为 DDP + 60 天账期，三项各打 ⚠。Rev1 改为 15+5 箱共 800、FOB、定金/尾款条款，标记「已对齐全 ✓，但收款与图稿未确认，不可投产」。交互演示：重复提交 Rev1 显示「已存在，未重建」（幂等）；上传 Rev2 时状态变为「新版本，必须人工复核」。

## 5. Design Tokens 与关键 CSS

```css
:root{
  --bg:#fcfbf8; --ink:#1c1c1c; --line:#e6e2da;
  --card:#ffffff; --radius:18px; --muted:#6b675f;
  --grad:linear-gradient(135deg,#ff8a3d,#ff5e8a 55%,#9b5de5);
  --warn:#d9730d;
}
body{background:var(--bg);color:var(--ink);}
.case-wrap{max-width:720px;margin:0 auto;padding:40px 20px 80px;}
.card{background:var(--card);border:1px solid var(--line);
      border-radius:var(--radius);padding:24px;}
.notice{background:#f3f0ea;border:1px solid var(--line);
        border-radius:12px;padding:12px 16px;font-size:14px;color:var(--muted);}
.btn-primary{background:var(--ink);color:#fff;border:none;
  border-radius:12px;padding:12px 22px;font-size:15px;cursor:pointer;}
.btn-primary:hover{opacity:.88}
.btn-primary:focus-visible,.toggle:focus-visible{
  outline:2px solid var(--ink);outline-offset:3px;}
.steps{display:flex;gap:8px;}
.step{flex:1;border:1px solid var(--line);border-radius:12px;
      padding:10px;text-align:center;font-size:14px;color:var(--muted);}
.step.human{border:2px solid transparent;
  background:linear-gradient(#fff,#fff) padding-box,var(--grad) border-box;
  color:var(--ink);}
.raw{font-family:ui-monospace,Menlo,monospace;font-size:13.5px;
     line-height:1.7;white-space:pre-wrap;}
.raw.collapsed{max-height:10.5em;overflow:hidden;
  -webkit-mask-image:linear-gradient(#000 70%,transparent);}
.flag{border-left:3px solid var(--warn);padding-left:10px;}
.badge{display:inline-block;font-size:12px;color:var(--warn);
  border:1px solid currentColor;border-radius:999px;padding:1px 8px;}
.internal{background:#f6f4ef;border-style:dashed;} /* 成本隔离区 */
.status{font-size:14px;padding:8px 12px;border-radius:10px;}
.status.ok{background:#eaf5ec;color:#1e6b34;}
.status.hold{background:#fdf1e3;color:var(--warn);}
```

## 6. 交互与状态（原生 JS，无框架）

- **展开原文**：按钮 + `aria-expanded` + `aria-controls`，切换 `.collapsed` 类，按钮文字同步变化；不用 `<details>` 以便控制渐隐遮罩。
- **订单页版本演示**：三个按钮（提交 Rev0 / Rev1 / Rev2）写入对应 `.status` 区域文案与 class，用 `role="status"` `aria-live="polite"` 播报结果；焦点在操作后移到状态区（`tabindex="-1"` + `focus()`）。
- 徽标 ⚠ 与状态色**均配合文字**，不单独依赖颜色传达。

## 7. 移动端（375px）

- `.case-wrap`  padding 减为 `24px 16px 64px`；卡片 padding 20px。
- 步骤条三段改竖排（`flex-direction:column`），箭头「→」改「↓」。
- 字段表由两列（标签/值）改单列堆叠，标签在上、值在下。
- 等宽原文允许横向滚动：`overflow-x:auto`，字号降至 12.5px。
- 按钮宽 100%、高 ≥48px；所有可点目标 ≥44px。

## 8. 键盘与无障碍

- 全页线性 Tab 序：返回链接 → 展开按钮 → 操作按钮，无 `tabindex` 正值。
- `:focus-visible` 统一黑色 2px 描边（见上 CSS），渐变边框的 `.step.human` 内元素聚焦时描边置于外侧不被裁剪。
- H1 唯一，区块用 H2（原始材料 / 整理结果 / 人工接手），步骤条用 `<ol>`。
- 声明条用普通文本即可，不用 `role="alert"`，避免朗读打断；状态变化才用 `aria-live`。

整体不引入任何库，三页共用一段约 40 行的 `case.css` 与 20 行 `case.js`，与原站静态结构直接拼接即可上线。
