package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const seedanceBackfillOptionKey = "SeedanceLogBackfillDone"

// BackfillSeedanceTaskLogs 一次性回填旧 seedance 任务日志的 task_id/billing_stage
// （三行透明化改造之前的历史行），使其可被单行化展示与 task_info 增强。
// options 表打标记保证幂等；异步执行不阻塞启动。
func BackfillSeedanceTaskLogs() {
	var opt Option
	if err := DB.Where(&Option{Key: seedanceBackfillOptionKey}).First(&opt).Error; err == nil && opt.Value == "true" {
		return
	}
	go func() {
		updated, err := runSeedanceLogBackfill()
		if err != nil {
			common.SysError("seedance log backfill aborted: " + err.Error())
			return
		}
		if err := UpdateOption(seedanceBackfillOptionKey, "true"); err != nil {
			common.SysError("seedance log backfill: failed to persist flag: " + err.Error())
			return
		}
		common.SysLog(fmt.Sprintf("seedance log backfill done, %d log rows updated", updated))
	}()
}

// runSeedanceLogBackfill 核心逻辑：分批遍历 tasks，按 private_data.request_id
// 定位 seedance 日志行，注入缺失的 task_id 与推断的 billing_stage
// （type=退款→refund；type=消费且有 pre_consumed_quota→settle；否则 pre_consume）。
// 单行失败记日志继续；返回实际更新行数。
func runSeedanceLogBackfill() (int, error) {
	const batch = 500
	updated := 0
	lastId := int64(0)
	for {
		var tasks []*Task
		if err := DB.Where("id > ?", lastId).Order("id asc").Limit(batch).Find(&tasks).Error; err != nil {
			return updated, err
		}
		if len(tasks) == 0 {
			return updated, nil
		}
		for _, tk := range tasks {
			lastId = tk.ID
			modelName := tk.Properties.OriginModelName
			if bc := tk.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
				modelName = bc.OriginModelName
			}
			if !strings.Contains(modelName, "seedance") {
				continue
			}
			reqId := tk.PrivateData.RequestId
			if reqId == "" || tk.TaskID == "" {
				continue
			}
			var logs []*Log
			if err := LOG_DB.Where("request_id = ? AND model_name LIKE ?", reqId, "%seedance%").Find(&logs).Error; err != nil {
				common.SysError("seedance backfill: query logs for " + reqId + ": " + err.Error())
				continue
			}
			for _, l := range logs {
				if l.Type != LogTypeConsume && l.Type != LogTypeRefund {
					continue
				}
				m, err := common.StrToMap(l.Other)
				if err != nil || m == nil {
					m = map[string]interface{}{}
				}
				changed := false
				if _, ok := m["task_id"]; !ok {
					m["task_id"] = tk.TaskID
					changed = true
				}
				if _, ok := m["billing_stage"]; !ok {
					stage := "pre_consume"
					if l.Type == LogTypeRefund {
						stage = "refund"
					} else if _, hasPre := m["pre_consumed_quota"]; hasPre {
						stage = "settle"
					}
					m["billing_stage"] = stage
					changed = true
				}
				if !changed {
					continue
				}
				if err := LOG_DB.Model(&Log{}).Where("id = ?", l.Id).Update("other", common.MapToJsonStr(m)).Error; err != nil {
					common.SysError(fmt.Sprintf("seedance backfill: update log %d: %s", l.Id, err.Error()))
					continue
				}
				updated++
			}
		}
	}
}
