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
package controller

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"

	"github.com/gin-gonic/gin"
)

// availabilityCacheTTL 是聚合结果的进程内缓存时长。
//
// 前端 staleTime 60s + refetchInterval 5min，60 秒缓存把真实查询频率
// 压到约每分钟一次，与管理员开着的标签页数量无关。
const availabilityCacheTTL = 60 * time.Second

type availabilityCacheEntry struct {
	payload  model.AvailabilityResponse
	cachedAt time.Time
}

var (
	availabilityCacheMu sync.Mutex
	availabilityCache   = map[string]availabilityCacheEntry{}
)

// GetAvailabilityStatus 返回 24 小时可用性总览。
func GetAvailabilityStatus(c *gin.Context) {
	dimension := c.Query("dimension")
	if dimension != "model" {
		dimension = "group"
	}

	if payload, ok := loadAvailabilityCache(dimension); ok {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": payload})
		return
	}

	// 采集关闭时明确告知前端，避免页面显示一个语焉不详的空状态。
	// 该分支不写缓存：开关一旦打开应立即生效。
	if !perf_metrics_setting.GetSetting().Enabled {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": model.AvailabilityResponse{
			GeneratedAt:     time.Now().Unix(),
			Dimension:       dimension,
			MetricsDisabled: true,
			Entities:        []model.AvailabilityEntity{},
		}})
		return
	}

	now := time.Now().Unix()
	startTs := model.AvailabilityStartTime(now)
	rows, err := model.QueryAvailabilityRows(startTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 合并未落库的热桶：当前小时的桶不会被 flush 写入，只查库会让
	// 最右侧的点永远是空的，current 也只能看到已完结的小时。
	// 先查库再读热桶：若某个桶恰在两次读取之间被 flush 落库，
	// 这一小时的数据会短暂缺失（下次刷新即恢复），而反序会造成重复计数。
	rows = append(rows, perfmetrics.HotAvailabilityRows(startTs)...)

	payload := model.BuildAvailability(rows, dimension, now)
	storeAvailabilityCache(dimension, payload)

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": payload})
}

// GetAvailabilityRpm 返回当前节点最近 60 秒的请求数。
//
// 单独成一个轻接口：前端每 10 秒轮询一次，不应为此重跑聚合查询。
func GetAvailabilityRpm(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"rpm": perfmetrics.CurrentRpm()},
	})
}

func loadAvailabilityCache(dimension string) (model.AvailabilityResponse, bool) {
	availabilityCacheMu.Lock()
	defer availabilityCacheMu.Unlock()

	entry, ok := availabilityCache[dimension]
	if !ok || time.Since(entry.cachedAt) > availabilityCacheTTL {
		return model.AvailabilityResponse{}, false
	}
	return entry.payload, true
}

func storeAvailabilityCache(dimension string, payload model.AvailabilityResponse) {
	availabilityCacheMu.Lock()
	defer availabilityCacheMu.Unlock()

	availabilityCache[dimension] = availabilityCacheEntry{
		payload:  payload,
		cachedAt: time.Now(),
	}
}
