# 汇总账单 Phase 5 — 账单管理菜单显隐开关（运营设置） 设计方案

- 日期: 2026-07-02
- 状态: 已确认设计，待写实现计划
- 关联: 增量于 Phase 1-4（账单功能）
- 相关规则: CLAUDE.md Rule 3 (Bun)、Rule 5 (受保护标识)

## 1. 背景与目标

在运营设置的「侧边栏模块」配置里，为「账单管理」菜单加一个显隐开关，放在个人中心的「个人设置」之后。两端 (`web/default` + `web/classic`) 都要有，且开关生效。开关默认开启（保持现状可见，管理员可关）。

## 2. 现有机制（探索结论）

两端 sidebar 显隐由后端选项 `SidebarModulesAdmin`（JSON 字符串，运营设置配置）驱动，结构 `{ section: { enabled, module... } }`。账单菜单属 `personal` 分组。**无后端改动**——新增的是该 JSON 里的一个 module key，由前端默认配置补齐。

- **新版 (`web/default`)**：
  - 运营设置 UI `maintenance/sidebar-modules-section.tsx` 数据驱动（遍历配置渲染开关）。
  - 默认配置在 `maintenance/config.ts` 的 `SIDEBAR_MODULES_DEFAULT` 与 `hooks/use-sidebar-config.ts` 的 `DEFAULT_SIDEBAR_MODULES`（两份需同步）。
  - sidebar 过滤靠 `use-sidebar-config.ts` 的 `URL_TO_CONFIG_MAP`（URL→{section,module}）。当前 `/bill-management` 不在映射表 → 开关对它无效（恒显）。需加映射。
- **旧版 (`web/classic`)**：
  - `hooks/common/useSidebar.js` 的 `DEFAULT_ADMIN_CONFIG.personal` 已有 `bill: true`（Phase 之前菜单修复时加），过滤已生效。
  - 运营设置 UI `pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx` 硬编码 section/module 列表，无 bill 开关。需补。

## 3. 已确认决策

| 主题 | 决策 |
|---|---|
| 默认值 | `bill: true`（默认开启，保持现状；管理员可关）。 |
| 位置 | 个人中心分组内，放在「个人设置」(personal) 之后。 |
| 新版映射 | `URL_TO_CONFIG_MAP` 加 `/bill-management → {personal, bill}`，使开关真正生效。 |
| 后端 | 无改动（复用 SidebarModulesAdmin 选项）。 |

## 4. 改动清单

### 新版 (`web/default`)
1. `features/system-settings/maintenance/config.ts`：`SIDEBAR_MODULES_DEFAULT.personal` 加 `bill: true`（在 `personal` 后）。
2. `hooks/use-sidebar-config.ts`：
   - `DEFAULT_SIDEBAR_MODULES.personal` 加 `bill: true`（在 `personal` 后）。
   - `URL_TO_CONFIG_MAP` 加 `'/bill-management': { section: 'personal', module: 'bill' }`。
3. `features/system-settings/maintenance/sidebar-modules-section.tsx`：`moduleMeta.personal` 加 `bill` 标签（title「Bill Management」+ 描述），在 `personal` 后。
4. i18n：新版 `zh.json` 若描述用到新英文 key 则补（`Bill Management` 已有）。

### 旧版 (`web/classic`)
5. `pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx`：personal 分组 4 处补 `bill`：
   - 初始 state 默认（约 56-59 行）personal 对象加 `bill: true`。
   - `resetSidebarModules` 默认（约 117-120 行）personal 加 `bill: true`。
   - `useEffect` catch 兜底默认（约 190 行）personal 加 `bill: true`。
   - `sectionConfigs` 的 personal.modules 数组（约 242-248）加 `{ key: 'bill', title: t('账单管理'), description: t('汇总账单查询与导出') }`，在「个人设置」后。
6. `hooks/common/useSidebar.js` 已有 `bill: true`，无需改。
7. i18n：旧版语言文件（key 即中文）补 `账单管理`（已有）、`汇总账单查询与导出`（新增描述）。

## 5. 语义与兼容

- 默认开启，不改变现状。管理员在运营设置关闭 → 两端账单菜单隐藏。
- 新版 `mergeWithDefaultSidebarModules` / `parseSidebarModulesAdmin` 会把新 key 合并进旧的已保存配置（缺失字段回退默认 true），故历史配置不会锁死账单菜单。
- 旧版 `useSidebar.js` 的 `finalConfig` 合成里 `userAllowed` 默认 true（除非显式 false），历史配置同样不锁死。

## 6. 测试策略

- 两端 `bun run build` 通过。
- 手动验证：运营设置「侧边栏模块 → 个人中心」出现「账单管理」开关（默认开）；关闭并保存后，两端侧边栏账单菜单消失；重新开启后恢复。

## 7. 范围外 (YAGNI)

- 不改后端。
- 不加用户级（user overlay）单独开关（沿用现有 admin×user 两层机制，默认行为足够）。
