/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * Zod schemas for common logs
 * This file should only contain Zod schemas and types inferred from them
 */
import { z } from 'zod'

// seedance 视频任务预扣行的查询时增强载荷（后端 model.LogTaskInfo）。
// 存在时使用日志把该行渲染为"单行任务视图"：状态列、进度条、三态费用与详情区块。
export const logTaskInfoSchema = z.object({
  status: z.string(),
  progress: z.string().optional(),
  fail_reason: z.string().optional(),
  task_id: z.string().optional(),
  upstream_task_id: z.string().optional(),
  pre_quota: z.number().default(0),
  final_quota: z.number().default(0),
  output_tokens: z.number().optional(),
  resolution_tier: z.string().optional(),
  resolution: z.string().optional(),
  ratio: z.string().optional(),
  duration_s: z.number().optional(),
  has_input: z.boolean().default(false),
  effective_ratio: z.number().optional(),
  is_user_ratio: z.boolean().optional(),
})

export type LogTaskInfo = z.infer<typeof logTaskInfoSchema>

// Usage log schema
export const usageLogSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  created_at: z.number(),
  type: z.number(),
  content: z.string(),
  username: z.string().default(''),
  token_name: z.string().default(''),
  model_name: z.string().default(''),
  quota: z.number().default(0),
  prompt_tokens: z.number().default(0),
  completion_tokens: z.number().default(0),
  use_time: z.number().default(0),
  is_stream: z.boolean().default(false),
  channel: z.number().default(0),
  channel_name: z.string().nullish().default(''),
  token_id: z.number().default(0),
  group: z.string().default(''),
  ip: z.string().default(''),
  other: z.string().default(''),
  request_id: z.string().default(''),
  upstream_request_id: z.string().default(''),
  task_info: logTaskInfoSchema.nullish(),
})

export type UsageLog = z.infer<typeof usageLogSchema>
