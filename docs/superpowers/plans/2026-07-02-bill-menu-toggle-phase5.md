# 汇总账单 Phase 5 (账单菜单显隐开关) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在运营设置的侧边栏模块配置里，为「账单管理」菜单加显隐开关(个人中心分组、放个人设置之后、默认开启)，两端生效。

**Architecture:** 复用现有 SidebarModulesAdmin 机制。新版在两份默认配置加 bill:true + URL_TO_CONFIG_MAP 加 /bill-management 映射 + moduleMeta 加标签；旧版在硬编码的运营设置 UI 补 bill 到 personal 分组。无后端改动。

**Tech Stack:** React 19 + Base UI (web/default)；React 18 + Semi (web/classic)；bun。

## Global Constraints

- 前端包管理器用 `bun`，命令在对应 web 目录执行(Rule 3)。
- 禁止修改/删除 QuantumNous / new-api 品牌标识；保留版权头(Rule 5)。
- 默认值 `bill: true`(默认开启)；位置在个人中心分组「个人设置」(personal) 之后。
- 无后端改动。

---

## Task 1: 新版默认配置 + 映射 + 标签

**Files:**
- Modify: `web/default/src/features/system-settings/maintenance/config.ts`
- Modify: `web/default/src/hooks/use-sidebar-config.ts`
- Modify: `web/default/src/features/system-settings/maintenance/sidebar-modules-section.tsx`

**Interfaces:**
- Produces: 运营设置「侧边栏模块 → 个人中心」出现「账单管理」开关；`/bill-management` URL 受该开关控制。

- [ ] **Step 1: config.ts 默认加 bill**

在 `web/default/src/features/system-settings/maintenance/config.ts` 的 `SIDEBAR_MODULES_DEFAULT.personal`：
```ts
  personal: {
    enabled: true,
    topup: true,
    personal: true,
  },
```
改为：
```ts
  personal: {
    enabled: true,
    topup: true,
    personal: true,
    bill: true,
  },
```

- [ ] **Step 2: use-sidebar-config.ts 默认加 bill + URL 映射**

在 `web/default/src/hooks/use-sidebar-config.ts` 的 `DEFAULT_SIDEBAR_MODULES.personal`：
```ts
  personal: {
    enabled: true,
    topup: true,
    personal: true,
  },
```
改为：
```ts
  personal: {
    enabled: true,
    topup: true,
    personal: true,
    bill: true,
  },
```

同文件 `URL_TO_CONFIG_MAP` 里，在 `'/profile': { section: 'personal', module: 'personal' },` 之后加：
```ts
  '/bill-management': { section: 'personal', module: 'bill' },
```

- [ ] **Step 3: sidebar-modules-section.tsx 加标签**

在 `web/default/src/features/system-settings/maintenance/sidebar-modules-section.tsx` 的 `moduleMeta.personal`：
```ts
    personal: {
      topup: {
        title: t('Wallet'),
        description: t('Top up balance and view billing history.'),
      },
      personal: {
        title: t('Profile'),
        description: t('Personal settings and profile management.'),
      },
    },
```
改为（在 personal 后加 bill）：
```ts
    personal: {
      topup: {
        title: t('Wallet'),
        description: t('Top up balance and view billing history.'),
      },
      personal: {
        title: t('Profile'),
        description: t('Personal settings and profile management.'),
      },
      bill: {
        title: t('Bill Management'),
        description: t('Summary bill query and export.'),
      },
    },
```

- [ ] **Step 4: i18n（若缺失）**

`web/default/src/i18n/locales/zh.json`：确认/补充键（`Bill Management` 已存在；新增描述键）：
```json
  "Summary bill query and export.": "汇总账单查询与导出。"
```
（只加缺失键，勿重复。）

- [ ] **Step 5: 构建**

Run（在 `web/default/`）: `bun run build` → 成功。

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/system-settings/maintenance/config.ts web/default/src/hooks/use-sidebar-config.ts web/default/src/features/system-settings/maintenance/sidebar-modules-section.tsx web/default/src/i18n/locales/zh.json
git commit -m "feat(bill): add bill management sidebar toggle in default operation settings

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: 旧版运营设置补 bill 开关

**Files:**
- Modify: `web/classic/src/pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx`
- Modify: `web/classic/src/i18n/locales/zh.json`（补描述键，若缺失）

**Interfaces:**
- Produces: 旧版运营设置「侧边栏模块 → 个人中心区域」出现「账单管理」开关；`useSidebar.js` 已有 bill:true 使菜单过滤生效。

- [ ] **Step 1: 初始 state 默认 personal 加 bill**

在 `web/classic/src/pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx`（约 56-60 行）：
```js
    personal: {
      enabled: true,
      topup: true,
      personal: true,
    },
```
改为：
```js
    personal: {
      enabled: true,
      topup: true,
      personal: true,
      bill: true,
    },
```

- [ ] **Step 2: resetSidebarModules 默认加 bill**

同文件 `resetSidebarModules`（约 117-121 行）：
```js
      personal: {
        enabled: true,
        topup: true,
        personal: true,
      },
```
改为：
```js
      personal: {
        enabled: true,
        topup: true,
        personal: true,
        bill: true,
      },
```

- [ ] **Step 3: useEffect catch 兜底默认加 bill**

同文件 useEffect 的 catch 兜底（约 190 行）：
```js
          personal: { enabled: true, topup: true, personal: true },
```
改为：
```js
          personal: { enabled: true, topup: true, personal: true, bill: true },
```

- [ ] **Step 4: sectionConfigs 的 personal.modules 加账单项**

同文件 `sectionConfigs` 里 personal 分组的 modules（约 242-249 行）：
```js
      modules: [
        { key: 'topup', title: t('钱包管理'), description: t('余额充值管理') },
        {
          key: 'personal',
          title: t('个人设置'),
          description: t('个人信息设置'),
        },
      ],
```
改为（在个人设置后加账单管理）：
```js
      modules: [
        { key: 'topup', title: t('钱包管理'), description: t('余额充值管理') },
        {
          key: 'personal',
          title: t('个人设置'),
          description: t('个人信息设置'),
        },
        {
          key: 'bill',
          title: t('账单管理'),
          description: t('汇总账单查询与导出'),
        },
      ],
```

- [ ] **Step 5: i18n（若缺失）**

`web/classic/src/i18n/locales/zh.json`（key 即中文）：确认/补充 `汇总账单查询与导出`（`账单管理` 已在之前 Phase 加过）。只加缺失键。

- [ ] **Step 6: 构建**

Run（在 `web/classic/`）: `bun run build` → 成功。

- [ ] **Step 7: Commit**

```bash
git add web/classic/src/pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx web/classic/src/i18n/locales/zh.json
git commit -m "feat(bill): add bill management sidebar toggle in classic operation settings

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Manual Verification

- [ ] 两端 `bun run build` 成功。
- [ ] 新版运营设置「侧边栏模块 → 个人中心」有「账单管理」开关，默认开；关闭保存后侧边栏个人组账单菜单消失；重开恢复。
- [ ] 旧版运营设置「侧边栏模块 → 个人中心区域」有「账单管理」开关，默认开；关闭保存后 `/console/bill` 菜单消失；重开恢复。
- [ ] 未配置过 SidebarModulesAdmin 的站点：账单菜单默认可见（默认 true）。

## Self-Review Notes

- **Spec 覆盖**：新版默认配置×2 + URL 映射 + 标签 (Task 1)；旧版硬编码 UI ×4 处 (Task 2)。
- **两份默认同步**：新版 config.ts 与 use-sidebar-config.ts 都加 bill（两处独立默认，必须一致）。
- **映射是关键**：URL_TO_CONFIG_MAP 加 /bill-management，否则新版开关对账单菜单无效(恒显)。
- **默认 true**：不改变现状；历史已保存配置经 merge 回退默认 true，不锁死。
- **YAGNI**：无后端；不加用户级单独开关。
